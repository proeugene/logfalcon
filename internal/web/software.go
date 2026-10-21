package web

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/proeugene/logfalcon/internal/update"
)

func (s *Server) handleSoftware(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.sendHTML(w, r, 200, RenderSoftware(s.Version, s.BuildCommit, s.updateABI, s.csrfToken, "", "", ""))
}
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	state := update.ReadState(s.updateStateDir)
	if _, err := os.Stat(filepath.Join(s.updateInbox, "request")); err == nil && state.Message != "Update state unreadable; inspect diagnostics." {
		state = update.State{Phase: "queued", Message: "Waiting for exclusive installation lock. Installation is not yet confirmed."}
	}
	s.sendJSON(w, r, 200, state)
}
func (s *Server) handleSoftwarePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, update.MaxUpload+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.sendError(w, r, 400, "Invalid or oversized update upload.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if r.FormValue("csrf_token") != s.csrfToken {
		s.sendError(w, r, 403, "The appliance restarted or this page expired. Reload Software update and try again.")
		return
	}
	lock, err := os.OpenFile(filepath.Join(s.updateInbox, "upload.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		s.sendError(w, r, 503, "Updater is not installed. Use the documented bootstrap.")
		return
	}
	defer func() { _ = lock.Close() }()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		s.sendError(w, r, 409, "Another upload is active.")
		return
	}
	state := update.ReadState(s.updateStateDir)
	if state.Message == "Update state unreadable; inspect diagnostics." {
		s.sendError(w, r, 503, "Update recovery state is unreadable. Administrator recovery is required.")
		return
	}
	if state.Phase == "activating" || state.Phase == "restarting" || state.Phase == "validating" {
		s.sendError(w, r, 409, "An update is in progress.")
		return
	}
	path := filepath.Join(s.updateInbox, "package.lfu")
	if _, err = os.Stat(filepath.Join(s.updateInbox, "request")); !os.IsNotExist(err) {
		s.sendError(w, r, 409, "An installation is queued.")
		return
	}
	if r.FormValue("install") == "1" {
		f, err := os.Open(path)
		if err != nil {
			s.sendError(w, r, 400, "Upload the update again.")
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			s.sendError(w, r, 400, "Upload unreadable.")
			return
		}
		keys, err := update.Keys(s.updateKeyDir)
		if err != nil {
			s.sendError(w, r, 503, "Trusted update keys are not installed.")
			return
		}
		pkg, err := update.Verify(f, info.Size(), keys, s.updateABI, s.Version)
		if err != nil {
			s.sendError(w, r, 400, err.Error())
			return
		}
		if pkg.Manifest.Commit != r.FormValue("commit") {
			s.sendError(w, r, 409, "Upload changed. Review it again.")
			return
		}
		if err = os.WriteFile(filepath.Join(s.updateInbox, "request"), []byte("install\n"), 0600); err != nil {
			s.sendError(w, r, 500, "Could not queue update.")
			return
		}
		http.Redirect(w, r, "/software", http.StatusSeeOther)
		return
	}
	source, _, err := r.FormFile("package")
	if err != nil {
		s.sendError(w, r, 400, "Select an update file.")
		return
	}
	defer func() { _ = source.Close() }()
	f, err := os.CreateTemp(s.updateInbox, "upload-*")
	if err != nil {
		s.sendError(w, r, 500, "Could not stage upload.")
		return
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	n, err := io.Copy(f, io.LimitReader(source, update.MaxUpload+1))
	if err != nil || n > update.MaxUpload {
		s.sendError(w, r, 400, "Update file exceeds the limit.")
		return
	}
	keys, err := update.Keys(s.updateKeyDir)
	if err != nil {
		s.sendError(w, r, 503, "Trusted update keys are not installed.")
		return
	}
	pkg, err := update.Verify(f, n, keys, s.updateABI, s.Version)
	if err != nil {
		s.sendError(w, r, 400, err.Error())
		return
	}
	if err = f.Sync(); err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		s.sendError(w, r, 500, "Could not save upload.")
		return
	}
	s.sendHTML(w, r, 200, RenderSoftware(s.Version, s.BuildCommit, s.updateABI, s.csrfToken, pkg.Manifest.Version, pkg.Manifest.Commit, pkg.Manifest.Architecture))
}

// Startup runs after the previous web process has stopped. Only its private
// staging names are removed; queued packages and root recovery evidence remain.
func (s *Server) cleanAbandonedUploads() {
	root, err := os.OpenRoot(s.updateInbox)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return
	}
	defer func() { _ = dir.Close() }()
	for {
		entries, err := dir.ReadDir(64)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "upload-") && entry.Type().IsRegular() {
				_ = root.Remove(entry.Name())
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) configureUpdates() {
	s.updateInbox = update.Inbox
	s.updateStateDir = update.StateDir
	s.updateKeyDir = update.KeyDir
	s.updateABI = update.ABI()
}

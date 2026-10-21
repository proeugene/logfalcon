package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/proeugene/logfalcon/internal/storage"
)

const maxExportRecordings = 100

type exportEntry struct {
	name string
	file *os.File
	data []byte
	size int64
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	fail := func(code int, message string) { s.sendJSON(w, r, code, map[string]string{"error": message}) }
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		fail(400, "Invalid selection. Refresh and try again.")
		return
	}
	if r.PostForm.Get("csrf_token") != s.csrfToken {
		fail(403, "This page expired. Reload before downloading.")
		return
	}
	ids := r.PostForm["session"]
	if len(ids) == 0 || len(ids) > maxExportRecordings {
		fail(400, "Select between 1 and 100 recordings.")
		return
	}
	entries := []exportEntry{}
	defer func() {
		for _, e := range entries {
			if e.file != nil {
				_ = e.file.Close()
			}
		}
	}()
	seen, controllers := map[string]bool{}, map[string]bool{}
	// Open and validate every selected recording before starting the response.
	// Held file handles remain readable if cleanup removes a session meanwhile.
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		manifest, err := storage.OpenSessionFile(s.storagePath, id, storage.ManifestFilename)
		if err != nil {
			fail(404, "A selected recording is unavailable. Refresh the list and select again.")
			return
		}
		data, err := io.ReadAll(io.LimitReader(manifest, (1<<20)+1))
		_ = manifest.Close()
		var m storage.Manifest
		if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &m) != nil || m.File.Bytes < 0 {
			fail(409, "A selected manifest is unreadable. No ZIP was created.")
			return
		}
		f, err := storage.OpenSessionFile(s.storagePath, id, storage.RawFlashFilename)
		if err != nil {
			fail(404, "A selected log file is unavailable. Refresh the list and select again.")
			return
		}
		entries = append(entries, exportEntry{name: id + "/raw_flash.bbl", file: f, size: m.File.Bytes})
		st, err := f.Stat()
		if err != nil || st.Size() != m.File.Bytes {
			fail(409, "A selected log does not match its manifest size. No ZIP was created.")
			return
		}
		entries = append(entries, exportEntry{name: id + "/manifest.json", data: data})
		notes, err := storage.ReadRecordingAnnotation(s.storagePath, id)
		if err != nil {
			fail(409, "Notes could not be read. Existing files are unchanged; no ZIP was created.")
			return
		}
		noteData, err := json.MarshalIndent(notes, "", "  ")
		if err != nil {
			fail(500, "Could not prepare notes.")
			return
		}
		entries = append(entries, exportEntry{name: id + "/pilot-notes.json", data: noteData})
		fc := strings.Split(id, "/")[0]
		if !controllers[fc] {
			controller, err := storage.ReadControllerAnnotation(s.storagePath, id)
			if err != nil {
				fail(409, "A controller name could not be read. No ZIP was created.")
				return
			}
			controllerData, err := json.MarshalIndent(controller, "", "  ")
			if err != nil {
				fail(500, "Could not prepare controller names.")
				return
			}
			entries = append(entries, exportEntry{name: fc + "/pilot-controller.json", data: controllerData})
			controllers[fc] = true
		}
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="logfalcon-recordings.zip"`)
	// Stream without a temporary archive or a whole-file buffer. Renew the write
	// deadline so a large download can run longer than the server's usual limit.
	zw := zip.NewWriter(&exportWriter{w: w, controller: http.NewResponseController(w)})
	buf := make([]byte, 64<<10)
	for _, e := range entries {
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: "LogFalcon/" + e.name, Method: zip.Store})
		if err == nil {
			var source io.Reader = bytes.NewReader(e.data)
			expected := int64(len(e.data))
			if e.file != nil {
				source = io.LimitReader(e.file, e.size+1)
				expected = e.size
			}
			var n int64
			n, err = io.CopyBuffer(entry, source, buf)
			if err == nil && n != expected {
				err = fmt.Errorf("recording size changed")
			}
		}
		if err != nil {
			abortExport(err)
		}
	}
	if err := zw.Close(); err != nil {
		abortExport(err)
	}
}

func abortExport(err error) {
	slog.Warn("recording ZIP interrupted", "error", err)
	// Do not turn an incomplete transfer into a valid, apparently complete ZIP.
	panic(http.ErrAbortHandler)
}

type exportWriter struct {
	w          http.ResponseWriter
	controller *http.ResponseController
	renewed    time.Time
}

func (w *exportWriter) Write(b []byte) (int, error) {
	if time.Since(w.renewed) > 10*time.Second {
		_ = w.controller.SetWriteDeadline(time.Now().Add(time.Minute))
		w.renewed = time.Now()
	}
	return w.w.Write(b)
}

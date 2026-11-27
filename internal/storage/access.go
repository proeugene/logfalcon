package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrSessionBusy = errors.New("session is in use")

// Both the collector and deletion hold the directory lock. It survives the
// partial-file rename and manifest replacements, and is released on exit.
func lockSessionDirectory(session *os.Root) (*os.File, error) {
	dir, err := session.Open(".")
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = dir.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrSessionBusy
		}
		return nil, err
	}
	return dir, nil
}

// SessionRoot pins directory handles inside storage. No session path component
// may be a symlink. os.Root also prevents escapes if paths change after checking.
func SessionRoot(storagePath, sessionID string) (*os.Root, error) {
	parts := strings.Split(sessionID, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session identifier")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `\:`) {
			return nil, fmt.Errorf("invalid session component")
		}
	}
	root, err := os.OpenRoot(storagePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{parts[0], filepath.Join(parts...)} {
		st, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("session path must be a real directory")
		}
	}
	return root.OpenRoot(filepath.Join(parts...))
}

func OpenSessionFile(storagePath, sessionID, filename string) (*os.File, error) {
	if filename != RawFlashFilename && filename != ManifestFilename {
		return nil, fmt.Errorf("file not downloadable")
	}
	root, err := SessionRoot(storagePath, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	st, err := root.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("file must be regular and not a symlink")
	}
	f, err := root.Open(filename)
	if err != nil {
		return nil, err
	}
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("file changed while opening")
	}
	return f, nil
}

func DeleteSession(storagePath, sessionID string) error {
	session, err := SessionRoot(storagePath, sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	dir, err := lockSessionDirectory(session)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	root, err := os.OpenRoot(storagePath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	// RemoveAll is traversal-resistant and does not follow a replaced final symlink.
	return root.RemoveAll(filepath.FromSlash(sessionID))
}

func openSessionPath(storagePath, dir string) (*os.Root, error) {
	rel, err := filepath.Rel(storagePath, dir)
	if err != nil {
		return nil, err
	}
	return SessionRoot(storagePath, filepath.ToSlash(rel))
}

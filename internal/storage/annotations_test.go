package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotationsRejectUnsafePathsAndCorruptFiles(t *testing.T) {
	for _, kind := range []string{"session-symlink", "controller-symlink", "notes-symlink", "controller-file-symlink", "corrupt", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			fc := filepath.Join(root, "fc_BTFL_uid-test")
			dir := filepath.Join(fc, "session")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			secret := filepath.Join(outside, "secret")
			original := `{"title":"outside"}`
			if err := os.WriteFile(secret, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "session-symlink":
				_ = os.Remove(dir)
				if err := os.Symlink(outside, dir); err != nil {
					t.Fatal(err)
				}
			case "controller-symlink":
				_ = os.RemoveAll(fc)
				if err := os.Symlink(outside, fc); err != nil {
					t.Fatal(err)
				}
			case "notes-symlink":
				if err := os.Symlink(secret, filepath.Join(dir, "pilot-notes.json")); err != nil {
					t.Fatal(err)
				}
			case "controller-file-symlink":
				if err := os.Symlink(secret, filepath.Join(fc, "pilot-controller.json")); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(filepath.Join(dir, "pilot-notes.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(filepath.Join(dir, "pilot-notes.json"), []byte(strings.Repeat("x", 16385)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			id := "fc_BTFL_uid-test/session"
			var err error
			if kind == "controller-file-symlink" {
				_, err = ReadControllerAnnotation(root, id)
				if err == nil {
					t.Fatal("unsafe read")
				}
				err = SaveControllerAnnotation(root, id, ControllerAnnotation{Nickname: "changed"})
			} else {
				_, err = ReadRecordingAnnotation(root, id)
				if err == nil {
					t.Fatal("unsafe read")
				}
				err = SaveRecordingAnnotation(root, id, RecordingAnnotation{Title: "changed"})
			}
			if err == nil {
				t.Fatal("unsafe write")
			}
			b, _ := os.ReadFile(secret)
			if string(b) != original {
				t.Fatal("outside file changed")
			}
		})
	}
}

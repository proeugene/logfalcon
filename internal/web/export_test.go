package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proeugene/logfalcon/internal/storage"
)

func TestSelectedRecordingsZIP(t *testing.T) {
	s, root, id := annotationFixture(t)
	other := "fc_BTFL_uid-12345678/2026-10-22_100000"
	if err := os.Mkdir(filepath.Join(root, other), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"raw_flash.bbl", "manifest.json"} {
		b, err := os.ReadFile(filepath.Join(root, id, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, other, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	notes := storage.RecordingAnnotation{Title: "Props <test>", Notes: "Line one\n第二次"}
	if err := storage.SaveRecordingAnnotation(root, id, notes); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveControllerAnnotation(root, id, storage.ControllerAnnotation{Nickname: "5-inch"}); err != nil {
		t.Fatal(err)
	}
	w := annotationRequest(s, "/download-selected", url.Values{"csrf_token": {s.csrfToken}, "session": {id, other, id}})
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export %d: %s", w.Code, w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 7 {
		t.Fatalf("got %d entries, want 7", len(z.File))
	}
	seen := map[string]bool{}
	for _, f := range z.File {
		if seen[f.Name] || strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			t.Fatal("unsafe or duplicated ZIP path")
		}
		seen[f.Name] = true
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(f.Name, "raw_flash.bbl"):
			if string(b) != "bbl" {
				t.Fatal("recording bytes changed")
			}
		case f.Name == "LogFalcon/"+id+"/pilot-notes.json":
			var a storage.RecordingAnnotation
			if json.Unmarshal(b, &a) != nil || a != notes {
				t.Fatal("notes missing from archive")
			}
		case strings.HasSuffix(f.Name, "pilot-controller.json"):
			if !strings.Contains(string(b), "5-inch") {
				t.Fatal("nickname missing")
			}
		}
	}
	for _, session := range []string{id, other} {
		for _, name := range []string{"raw_flash.bbl", "manifest.json", "pilot-notes.json"} {
			if !seen["LogFalcon/"+session+"/"+name] {
				t.Fatal("missing selected recording content")
			}
		}
	}
}

func TestSelectedZIPRejectsInvalidSelectionBeforeStreaming(t *testing.T) {
	for _, kind := range []string{"empty", "too-many", "csrf", "traversal", "session-symlink", "file-symlink", "notes-symlink", "missing", "size", "corrupt-notes"} {
		t.Run(kind, func(t *testing.T) {
			s, root, id := annotationFixture(t)
			ids := []string{id}
			token := s.csrfToken
			want := 404
			switch kind {
			case "empty":
				ids = nil
				want = 400
			case "too-many":
				ids = make([]string, 101)
				for i := range ids {
					ids[i] = id
				}
				want = 400
			case "csrf":
				token = "expired"
				want = 403
			case "traversal":
				ids = []string{"../outside"}
			case "session-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "fc_BTFL_uid-12345678", "linked")); err != nil {
					t.Fatal(err)
				}
				ids = []string{"fc_BTFL_uid-12345678/linked"}
			case "file-symlink", "notes-symlink":
				name := "raw_flash.bbl"
				if kind == "notes-symlink" {
					name = "pilot-notes.json"
					want = 409
				}
				target := filepath.Join(root, id, name)
				_ = os.Remove(target)
				if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), target); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(filepath.Join(root, id, "raw_flash.bbl")); err != nil {
					t.Fatal(err)
				}
			case "size":
				if err := os.WriteFile(filepath.Join(root, id, "raw_flash.bbl"), []byte("truncated"), 0600); err != nil {
					t.Fatal(err)
				}
				want = 409
			case "corrupt-notes":
				if err := os.WriteFile(filepath.Join(root, id, "pilot-notes.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
				want = 409
			}
			w := annotationRequest(s, "/download-selected", url.Values{"csrf_token": {token}, "session": ids})
			if w.Code != want || w.Header().Get("Content-Type") == "application/zip" || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
				t.Fatalf("got %d, want %d; incomplete ZIP exposed", w.Code, want)
			}
		})
	}
}

type brokenDownload struct{ header http.Header }

func (w *brokenDownload) Header() http.Header       { return w.header }
func (w *brokenDownload) WriteHeader(int)           {}
func (w *brokenDownload) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestSelectedZIPAbortsBrokenResponse(t *testing.T) {
	s, _, id := annotationFixture(t)
	v := url.Values{"csrf_token": {s.csrfToken}, "session": {id}}
	r := httptest.NewRequest("POST", "/download-selected", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("wanted aborted download, got %v", got)
		}
	}()
	s.ServeHTTP(&brokenDownload{header: make(http.Header)}, r)
}

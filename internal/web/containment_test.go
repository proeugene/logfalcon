package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadCannotEscapeStorage(t *testing.T) {
	for _, kind := range []string{"prefix-collision", "session-symlink", "file-symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, root := newTestServer(t)
			outside := root + "-outside"
			if kind != "prefix-collision" {
				outside = t.TempDir()
			}
			if err := os.MkdirAll(outside, 0755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(outside) })
			secret := filepath.Join(outside, "raw_flash.bbl")
			if err := os.WriteFile(secret, []byte("outside-secret"), 0600); err != nil {
				t.Fatal(err)
			}
			fc := filepath.Join(root, "fc_BTFL_uid-test")
			if err := os.MkdirAll(fc, 0755); err != nil {
				t.Fatal(err)
			}
			session := filepath.Join(fc, "session")
			if kind == "file-symlink" {
				if err := os.Mkdir(session, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(secret, filepath.Join(session, "raw_flash.bbl")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(outside, session); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("GET", "/download/fc_BTFL_uid-test/session/raw_flash.bbl", nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != 404 || strings.Contains(w.Body.String(), "outside-secret") {
				t.Fatalf("escaped via %s: status %d body %q", kind, w.Code, w.Body.String())
			}
		})
	}
}

func TestDeleteRejectsSessionSymlink(t *testing.T) {
	s, root := newTestServer(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "keep")
	if err := os.WriteFile(secret, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "fc_test"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "fc_test", "session")); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("DELETE", "/sessions/fc_test/session", nil)
	r.Header.Set("X-CSRF-Token", s.csrfToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("symlink deletion accepted")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("outside file changed")
	}
}

func TestDownloadStandardRanges(t *testing.T) {
	s, root := newTestServer(t)
	dir := filepath.Join(root, "fc_test", "session")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "raw_flash.bbl"), []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		rangeValue string
		code       int
		body       string
	}{{"bytes=-3", 206, "789"}, {"bytes=2-4", 206, "234"}, {"bytes=7-", 206, "789"}, {"bytes=100-", 416, ""}} {
		r := httptest.NewRequest("GET", "/download/fc_test/session/raw_flash.bbl", nil)
		r.Header.Set("Range", tc.rangeValue)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.code || (tc.code == 206 && w.Body.String() != tc.body) {
			t.Fatalf("range %s: %d %q", tc.rangeValue, w.Code, w.Body.String())
		}
	}
}

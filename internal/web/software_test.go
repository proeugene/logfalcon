package web

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/proeugene/logfalcon/internal/update"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineUploadReviewAndQueue(t *testing.T) {
	s, _ := newTestServer(t)
	root := t.TempDir()
	s.Version = "v0.4.6"
	s.BuildCommit = strings.Repeat("b", 40)
	s.updateABI = "arm6"
	s.updateInbox = filepath.Join(root, "inbox")
	s.updateStateDir = filepath.Join(root, "state")
	s.updateKeyDir = filepath.Join(root, "keys")
	for _, path := range []string{s.updateInbox, s.updateStateDir, s.updateKeyDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.updateKeyDir, "test.pub"), []byte(hex.EncodeToString(pub)), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "main.go")
	if err = os.WriteFile(source, []byte("package main\nimport \"fmt\"\nvar Version,BuildCommit string\nfunc main(){fmt.Println(Version,BuildCommit)}"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "binary")
	cmd := exec.Command("go", "build", "-ldflags", "-X main.Version=v0.5.0 -X main.BuildCommit="+strings.Repeat("a", 40), "-o", binary, source)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm", "GOARM=6", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for name := range update.Destinations {
		files[name] = []byte("release asset")
	}
	files["bin/logfalcon"] = executable
	path := filepath.Join(root, "release.lfu")
	m := update.Manifest{Format: 1, Version: "v0.5.0", Commit: strings.Repeat("a", 40), Architecture: "arm6", Base: update.Base, Migration: 1, KeyID: "test"}
	if err = update.Build(path, m, files, key); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	post := func(fields map[string]string, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if data != nil {
			part, err := writer.CreateFormFile("package", "release.lfu")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/software", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/software", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "v0.4.6") || !strings.Contains(w.Body.String(), "arm6") {
		t.Fatal("software page lacks installed identity")
	}
	if w = post(map[string]string{"csrf_token": "old"}, content); w.Code != 403 || !strings.Contains(w.Body.String(), "Reload") {
		t.Fatal("stale upload token not rejected")
	}
	if w = post(map[string]string{"csrf_token": s.csrfToken}, []byte("unsigned archive")); w.Code != 400 {
		t.Fatal("bad upload accepted")
	}
	if w = post(map[string]string{"csrf_token": s.csrfToken}, content); w.Code != 200 || !strings.Contains(w.Body.String(), "Review update") || !strings.Contains(w.Body.String(), "v0.5.0") {
		t.Fatalf("review %d %s", w.Code, w.Body.String())
	}
	if _, err = os.Stat(filepath.Join(s.updateInbox, "request")); !os.IsNotExist(err) {
		t.Fatal("upload activated without review")
	}
	if w = post(map[string]string{"csrf_token": s.csrfToken, "install": "1", "commit": "wrong"}, nil); w.Code != 409 {
		t.Fatal("changed review accepted")
	}
	if w = post(map[string]string{"csrf_token": s.csrfToken, "install": "1", "commit": m.Commit}, nil); w.Code != 303 {
		t.Fatalf("queue %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/software/status", nil))
	var state update.State
	if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil || state.Phase != "queued" {
		t.Fatalf("queued update misreported: %s", w.Body.String())
	}
	staged := filepath.Join(s.updateInbox, "upload-abandoned")
	if err = os.WriteFile(staged, []byte("staging"), 0600); err != nil {
		t.Fatal(err)
	}
	s.cleanAbandonedUploads()
	if _, err = os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("abandoned upload retained")
	}
	for _, name := range []string{"request", "package.lfu"} {
		if _, err = os.Stat(filepath.Join(s.updateInbox, name)); err != nil {
			t.Fatal("cleanup removed queued package")
		}
	}
}

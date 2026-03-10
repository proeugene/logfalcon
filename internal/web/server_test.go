package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.StoragePath = dir
	s := NewServer(dir, cfg)
	// Ensure idle sync status for deterministic tests.
	lfSync.SetStatus("idle", 0, "Ready for the next sync.")
	return s, dir
}

func TestHealthEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["ok"]; !ok {
		t.Error("missing 'ok' field")
	}
	if _, ok := body["uptime_sec"]; !ok {
		t.Error("missing 'uptime_sec' field")
	}
	if _, ok := body["status"]; !ok {
		t.Error("missing 'status' field")
	}
	if _, ok := body["session_count"]; !ok {
		t.Error("missing 'session_count' field")
	}
	if _, ok := body["storage"]; !ok {
		t.Error("missing 'storage' field")
	}
}

func TestSessionsEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body []any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON array: %v", err)
	}
}

func TestStatusEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	lfSync.SetStatus("syncing", 42, "Downloading flash...")

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["state"] != "syncing" {
		t.Errorf("expected state=syncing, got %v", body["state"])
	}
	progress, ok := body["progress"].(float64)
	if !ok || int(progress) != 42 {
		t.Errorf("expected progress=42, got %v", body["progress"])
	}
	if body["message"] != "Downloading flash..." {
		t.Errorf("expected message, got %v", body["message"])
	}
}

func TestCaptivePortal(t *testing.T) {
	s, _ := newTestServer(t)

	paths := []string{"/generate_204", "/gen_204", "/hotspot-detect.html", "/connecttest.txt", "/ncsi.txt"}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
		body := w.Body.String()
		if len(body) == 0 {
			t.Errorf("%s: empty body", path)
		}
		// Should contain a redirect to /
		if !containsStr(body, `url=/`) && !containsStr(body, `href="/"`) {
			t.Errorf("%s: body does not contain redirect to /", path)
		}
	}
}

func TestDownloadNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/download/invalid/raw_flash.bbl", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteWithoutCSRF(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/sessions/fc_BTFL_uid-abc/2025-01-01_120000", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ---------- additional coverage ----------

func TestDownloadValidFile(t *testing.T) {
	s, dir := newTestServer(t)

	// Create a session with a manifest.json
	fcDir := filepath.Join(dir, "fc_BTFL_uid-abc12345")
	sessDir := filepath.Join(fcDir, "2025-06-01_120000")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := storage.Manifest{
		Version:    1,
		CreatedUTC: "2025-06-01T12:00:00Z",
		FC:         storage.ManifestFC{Variant: "BTFL", UID: "abc12345", APIVersion: "1.46"},
		File:       storage.ManifestFile{Name: "raw_flash.bbl", SHA256: "deadbeef", Bytes: 1024},
	}
	mdata, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(sessDir, "manifest.json"), mdata, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/download/fc_BTFL_uid-abc12345/2025-06-01_120000/manifest.json", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("expected application/octet-stream, got %s", ct)
	}
}

func TestIndexPage(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !containsStr(body, "LogFalcon") {
		t.Error("index page should contain 'LogFalcon'")
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		findSubstring(haystack, needle))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

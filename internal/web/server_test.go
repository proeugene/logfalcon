package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	cfg.StatusPath = filepath.Join(t.TempDir(), "status.json")
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
	if _, ok := body["thermal"]; !ok {
		t.Error("missing 'thermal' field")
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
	lfSync.ConfigureStatus(s.config.StatusPath)
	t.Cleanup(func() { lfSync.ConfigureStatus("") })
	if err := lfSync.BeginTransfer("fake"); err != nil {
		t.Fatal(err)
	}
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

	tests := []struct {
		path           string
		wantStatus     int
		wantBodySubstr string // empty = no body check
	}{
		// iOS/macOS — success page
		{"/hotspot-detect.html", http.StatusOK, "Success"},
		{"/library/test/success.html", http.StatusOK, "Success"},
		// Android — 204 No Content, no body
		{"/generate_204", http.StatusNoContent, ""},
		{"/gen_204", http.StatusNoContent, ""},
		// Windows NCSI
		{"/ncsi.txt", http.StatusOK, "Microsoft NCSI"},
		{"/connecttest.txt", http.StatusOK, "Microsoft Connect Test"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)

		if w.Code != tt.wantStatus {
			t.Errorf("%s: expected %d, got %d", tt.path, tt.wantStatus, w.Code)
		}
		if tt.wantBodySubstr != "" && !containsStr(w.Body.String(), tt.wantBodySubstr) {
			t.Errorf("%s: body %q does not contain %q", tt.path, w.Body.String(), tt.wantBodySubstr)
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
	// The web process's local status cannot confirm a separate sync job's result.
	lfSync.SetStatus("idle", 0, "Sync complete — safe to unplug and fly again.")
	t.Cleanup(func() { lfSync.SetStatus("idle", 0, "Ready for the next sync.") })
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
	if !containsStr(body, "No transfer recorded this boot") {
		t.Error("index must use the shared file rather than unrelated local status")
	}
	if containsStr(body, "safe to unplug") {
		t.Error("index must not render unrelated process-local completion status")
	}
}

func TestHotspotSettingsTransaction(t *testing.T) {
	for _, fail := range []string{"", "restart", "config"} {
		t.Run(fail, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.hotspotPath = filepath.Join(t.TempDir(), "hostapd.conf")
			s.configPath = filepath.Join(t.TempDir(), "logfalcon.toml")
			host := "ssid=Old\nwpa_passphrase=oldpassword\ncountry_code=GB\n"
			cfg := "hotspot_ssid = \"Old\"\nhotspot_password = \"oldpassword\"\n"
			if err := os.WriteFile(s.hotspotPath, []byte(host), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.configPath, []byte(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			restarted := false
			s.restartHotspot = func() error {
				restarted = true
				if fail == "restart" {
					return errors.New("restart rejected")
				}
				return nil
			}
			if fail == "config" {
				s.configPath = filepath.Join(t.TempDir(), "missing")
			}
			form := url.Values{"csrf_token": {s.csrfToken}, "ssid": {"Pilot's hotspot"}, "password": {`quotes"and\slashes`}}
			r := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			b, _ := os.ReadFile(s.hotspotPath)
			if fail == "" {
				if w.Code != 200 || !restarted || !strings.Contains(string(b), "ssid=Pilot's hotspot") {
					t.Fatalf("save: %d %s", w.Code, w.Body.String())
				}
				loaded, err := config.Load(s.configPath)
				if err != nil || loaded.HotspotPassword != form.Get("password") {
					t.Fatal("invalid persisted TOML", err)
				}
			} else if w.Code != 500 || string(b) != host {
				t.Fatalf("failure did not roll back: %d %s", w.Code, b)
			}
		})
	}
}

func TestSessionEraseOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attempted bool
		completed bool
		want      string
	}{
		{"not attempted", false, false, "No erase recorded"},
		{"attempted without confirmation", true, false, "Erase unconfirmed"},
		{"completed", true, true, "FC erased"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := RenderSessions([]*storage.Session{{
				FCDir:      "fc_BTFL_uid-abc12345",
				SessionID:  "fc_BTFL_uid-abc12345/2026-10-02_120000",
				SessionDir: "2026-10-02_120000",
				Manifest: &storage.Manifest{
					EraseAttempted: tc.attempted,
					EraseCompleted: tc.completed,
				},
			}})
			if !containsStr(body, tc.want) {
				t.Fatalf("session should report %q", tc.want)
			}
			if !containsStr(body, "Log file unavailable") || containsStr(body, "Download .bbl") {
				t.Error("a manifest without a saved file must not offer a log download")
			}
		})
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

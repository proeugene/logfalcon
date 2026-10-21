package web

import (
	"errors"
	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/settings"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptionalSetupAndLiteralPassword(t *testing.T) {
	s, _ := newTestServer(t)
	s.restartHotspot = func() error { return nil }
	dir := t.TempDir()
	s.configPath = filepath.Join(dir, "logfalcon.toml")
	s.hotspotPath = filepath.Join(dir, "hostapd.conf")
	for path, b := range map[string]string{s.configPath: "hotspot_ssid = \"LogFalcon-123456\"\nhotspot_password = \"fpvpilot\"\n", s.hotspotPath: "ssid=LogFalcon-123456\nwpa_passphrase=fpvpilot\ncountry_code=GB\n"} {
		if err := os.WriteFile(path, []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/", nil); w.Code != 303 || w.Header().Get("Location") != "/settings" {
		t.Fatal("setup not offered")
	}
	w := request("GET", "/settings", nil)
	if strings.Contains(w.Body.String(), "fpvpilot") || !strings.Contains(w.Body.String(), `value="LogFalcon-123456"`) {
		t.Fatal("current password exposed or existing SSID not prefilled")
	}
	if w = request("POST", "/settings", url.Values{"csrf_token": {s.csrfToken}, "skip": {"1"}}); w.Code != 303 {
		t.Fatalf("skip: %d", w.Code)
	}
	if w = request("GET", "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Temporary default") {
		t.Fatal("skip failed persistence or reminder")
	}
	restarted := false
	s.restartHotspot = func() error { restarted = true; return nil }
	for _, pass := range []string{"  keep spaces  ", ""} {
		w = request("POST", "/settings", url.Values{"csrf_token": {s.csrfToken}, "ssid": {"New name"}, "password": {pass}, "country": {"DE"}})
		if w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		cfg, err := config.Load(s.configPath)
		if err != nil || cfg.HotspotPassword != "  keep spaces  " {
			t.Fatal("password trimmed or blank replacement saved")
		}
	}
	if readHostapdConfigAt(s.hotspotPath)["wpa_passphrase"] != "  keep spaces  " {
		t.Fatal("reading effective details trimmed the password")
	}
	if !restarted {
		t.Fatal("hotspot restart not requested")
	}
	w = request("POST", "/settings", url.Values{"csrf_token": {"stale"}, "ssid": {"Bad"}, "password": {"abcdefgh"}})
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Reload") {
		t.Fatal("stale CSRF not explained")
	}
}
func TestCompletedRefreshContract(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	page := w.Body.String()
	for _, required := range []string{"/sessions/fragment", "completedTransfer", "container.prepend(card)", "oldCards", "Checking saved copy", "http://192.168.4.1", "Software update"} {
		if !strings.Contains(page, required) {
			t.Fatalf("dashboard missing %s", required)
		}
	}
	if strings.Contains(page, "%!") {
		t.Fatal("malformed template substitution")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/sessions/fragment", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "No saved recordings") {
		t.Fatal("fragment unavailable")
	}
}

// Skipping setup is still a settings transaction: pending recovery must restart
// hostapd successfully before the journal can be cleared or setup marked done.
func TestSkipRequiresSuccessfulRecovery(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	s.configPath = filepath.Join(dir, "logfalcon.toml")
	s.hotspotPath = filepath.Join(dir, "hostapd.conf")
	for path, data := range map[string]string{
		s.configPath:  "hotspot_ssid = \"Original\"\nhotspot_password = \"safe-test-password\"\n",
		s.hotspotPath: "ssid=Original\nwpa_passphrase=safe-test-password\ncountry_code=GB\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := settings.Manager{Config: s.configPath, Hostapd: s.hotspotPath, Restart: func() error { return errors.New("restart failed") }}
	if err := m.Apply(settings.Values{SSID: "Changed"}); err == nil {
		t.Fatal("expected pending recovery")
	}
	journal := filepath.Join(dir, "settings-undo.json")
	restarts := 0
	s.restartHotspot = func() error { restarts++; return errors.New("still failing") }
	form := url.Values{"csrf_token": {s.csrfToken}, "skip": {"1"}}
	if w := annotationRequest(s, "/settings", form); w.Code != 500 {
		t.Fatalf("skip bypassed recovery: %d", w.Code)
	}
	if _, err := os.Stat(journal); err != nil || restarts == 0 {
		t.Fatalf("recovery not retained/restarted: %v, restarts=%d", err, restarts)
	}
	cfg, err := config.Load(s.configPath)
	if err != nil || cfg.SetupCompleted {
		t.Fatal("failed recovery marked setup complete")
	}
	s.restartHotspot = func() error { restarts++; return nil }
	if w := annotationRequest(s, "/settings", form); w.Code != 303 {
		t.Fatalf("retry failed: %d", w.Code)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("successful recovery retained journal")
	}
	cfg, err = config.Load(s.configPath)
	if err != nil || !cfg.SetupCompleted || cfg.HotspotSSID != "Original" || cfg.HotspotPassword != "safe-test-password" {
		t.Fatal("retry did not preserve credentials and complete setup")
	}
}

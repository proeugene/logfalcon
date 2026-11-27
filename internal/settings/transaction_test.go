package settings

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proeugene/logfalcon/internal/config"
)

func TestLegacyHashPreservesBrowserSettingsAndRequiresRestart(t *testing.T) {
	m := fixture(t)
	boot := filepath.Join(filepath.Dir(m.Config), "boot.txt")
	b := []byte("SSID=Old boot name\nPASSWORD=oldbootpassword\n")
	if err := os.WriteFile(boot, b, 0600); err != nil {
		t.Fatal(err)
	}
	m.LegacyHash = filepath.Join(filepath.Dir(m.Config), "legacy-hash")
	if err := os.WriteFile(m.LegacyHash, []byte(fmt.Sprintf("%x", sha256.Sum256(b))), 0600); err != nil {
		t.Fatal(err)
	}
	m.Restart = func() error { return errors.New("restart failed") }
	if err := m.ApplyBoot(boot); err == nil {
		t.Fatal("legacy marker hid failed restart")
	}
	if _, err := os.Stat(m.path("boot-applied.sha256")); !os.IsNotExist(err) {
		t.Fatal("failed restart migrated hash")
	}
	m.Restart = func() error { return nil }
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(m.Config)
	if err != nil || cfg.HotspotSSID != "Original" {
		t.Fatal("migration reverted newer browser settings")
	}
}

func fixture(t *testing.T) Manager {
	t.Helper()
	dir := t.TempDir()
	m := Manager{Hostapd: filepath.Join(dir, "hostapd.conf"), Config: filepath.Join(dir, "logfalcon.toml")}
	for path, data := range map[string]string{m.Hostapd: "ssid=Original\nwpa_passphrase=originalpass\ncountry_code=DE\n", m.Config: "hotspot_ssid = \"Original\"\nhotspot_password = \"originalpass\"\nerase_after_sync = false\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestRollbackAfterMutationAndRestartFailure(t *testing.T) {
	for _, failure := range []string{"second-write", "restart", "hash"} {
		t.Run(failure, func(t *testing.T) {
			m := fixture(t)
			beforeH, _ := os.ReadFile(m.Hostapd)
			beforeC, _ := os.ReadFile(m.Config)
			failed := false
			restarts := 0
			m.Write = func(path string, b []byte) error {
				if !failed && (failure == "second-write" && path == m.Config || failure == "hash" && strings.HasSuffix(path, "boot-applied.sha256")) {
					failed = true
					return errors.New("injected write failure")
				}
				return AtomicWrite(path, b)
			}
			m.Restart = func() error {
				restarts++
				if failure == "restart" && restarts == 1 {
					return errors.New("injected restart failure")
				}
				return nil
			}
			boot := filepath.Join(filepath.Dir(m.Config), "boot.txt")
			if err := os.WriteFile(boot, []byte("SSID=New\nPASSWORD=newpassword\nCOUNTRY_CODE=GB\nDEBUG=true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := m.ApplyBoot(boot); err == nil {
				t.Fatal("failure reported as success")
			}
			afterH, _ := os.ReadFile(m.Hostapd)
			afterC, _ := os.ReadFile(m.Config)
			if string(afterH) != string(beforeH) || string(afterC) != string(beforeC) {
				t.Fatal("pair not restored")
			}
			if _, err := os.Stat(m.path("boot-applied.sha256")); !os.IsNotExist(err) {
				t.Fatal("failed input marked applied")
			}
			if _, err := os.Stat(m.path("settings-undo.json")); !os.IsNotExist(err) {
				t.Fatal("successful recovery left journal")
			}
		})
	}
}

func TestInterruptedUpdateRecoveredAndConcurrentChangeRejected(t *testing.T) {
	m := fixture(t)
	peer := m
	m.Restart = func() error {
		if err := peer.Apply(Values{SSID: "Concurrent", Password: "otherpassword"}); err == nil {
			t.Error("concurrent writer accepted")
		}
		return errors.New("restart unavailable")
	}
	if err := m.Apply(Values{SSID: "New", Password: "newpassword"}); err == nil {
		t.Fatal("restart failure accepted")
	}
	if _, err := os.Stat(m.path("settings-undo.json")); err != nil {
		t.Fatal("failed recovery must retain journal")
	}
	// Next process recovers, before permitting another update.
	if err := peer.Recover(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(m.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HotspotSSID != "Original" {
		t.Fatal("recovery failed")
	}
}

func TestBootLiteralValuesAndUnchangedInputPreservesBrowserChanges(t *testing.T) {
	m := fixture(t)
	m.Restart = func() error { return nil }
	boot := filepath.Join(filepath.Dir(m.Config), "boot.txt")
	if err := os.WriteFile(boot, []byte("SSID=Pilot's hotspot\r\nPASSWORD=quotes\"and\\slashes\r\nCOUNTRY_CODE=GB\r\nDEBUG=false\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(m.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HotspotPassword != `quotes"and\slashes` || cfg.DebugLogging {
		t.Fatal("literal values lost")
	}
	if err = m.Apply(Values{SSID: "Browser", Password: "browserpassword"}); err != nil {
		t.Fatal(err)
	}
	if err = m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(m.Config)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HotspotSSID != "Browser" {
		t.Fatal("unchanged boot input reverted browser settings")
	}
	host, _ := os.ReadFile(m.Hostapd)
	if !strings.Contains(string(host), "country_code=GB") {
		t.Fatal("country lost")
	}
}

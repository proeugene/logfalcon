package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmedBootInputDoesNotRestartHotspot(t *testing.T) {
	m := fixture(t)
	boot := filepath.Join(filepath.Dir(m.Config), "boot.txt")
	if err := os.WriteFile(boot, []byte("SSID=Boot name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	restarts := 0
	m.Restart = func() error { restarts++; return nil }
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(Values{SSID: "Browser name"}); err != nil {
		t.Fatal(err)
	}
	restarts = 0
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	if restarts != 0 {
		t.Fatalf("unchanged, previously confirmed input restarted hotspot %d times", restarts)
	}
}

func TestUnchangedBootConfirmsServiceAndRejectsInvalidConfig(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "service-failure", true: "invalid-config"}[invalid], func(t *testing.T) {
			m := fixture(t)
			boot := filepath.Join(filepath.Dir(m.Config), "missing-boot.txt")
			calls := 0
			m.EnsureActive = func() error { calls++; return errors.New("hostapd failed") }
			m.Restart = func() error { t.Fatal("unchanged boot requested a restart"); return nil }
			if invalid {
				if err := os.WriteFile(m.Config, []byte("erase_after_sync = invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.ApplyBoot(boot); err == nil {
				t.Fatal("boot failure was accepted")
			}
			if invalid && calls != 0 || !invalid && calls != 1 {
				t.Fatalf("service confirmation calls = %d", calls)
			}
		})
	}
}

func TestUnchangedBootCannotBypassPendingRecovery(t *testing.T) {
	m := fixture(t)
	boot := filepath.Join(filepath.Dir(m.Config), "boot.txt")
	if err := os.WriteFile(boot, []byte("SSID=Boot name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.Restart = func() error { return nil }
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	m.Restart = func() error { return errors.New("restart unavailable") }
	if err := m.Apply(Values{SSID: "Interrupted browser change"}); err == nil {
		t.Fatal("expected a pending rollback")
	}
	m.EnsureActive = func() error { t.Fatal("pending rollback was bypassed"); return nil }
	if err := m.ApplyBoot(boot); err == nil {
		t.Fatal("recovery failure accepted")
	}
	if _, err := os.Stat(m.path("settings-undo.json")); err != nil {
		t.Fatal("failed recovery journal lost", err)
	}
	restarts := 0
	m.Restart = func() error { restarts++; return nil }
	m.EnsureActive = func() error { return nil }
	if err := m.ApplyBoot(boot); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("recovery did not confirm restart: %d", restarts)
	}
	if _, err := os.Stat(m.path("settings-undo.json")); !os.IsNotExist(err) {
		t.Fatal("successful recovery left journal", err)
	}
}

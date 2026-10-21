package settings

import (
	"github.com/proeugene/logfalcon/internal/config"
	"os"
	"strings"
	"testing"
)

func TestDeviceIdentityAndSetupPersistence(t *testing.T) {
	names := map[string]bool{}
	for range 8 {
		m := fixture(t)
		if err := m.Apply(Values{SSID: "LogFalcon", Password: "fpvpilot"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(m.path("identity.pending"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := m.InitialiseDevice(); err != nil {
			t.Fatal(err)
		}
		first, err := config.Load(m.Config)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(first.HotspotSSID, "LogFalcon-") || names[first.HotspotSSID] {
			t.Fatal("missing or reused identity")
		}
		names[first.HotspotSSID] = true
		if err := m.InitialiseDevice(); err != nil {
			t.Fatal(err)
		}
		if err := m.Apply(Values{SetupCompleted: true}); err != nil {
			t.Fatal(err)
		}
		second, err := config.Load(m.Config)
		if err != nil {
			t.Fatal(err)
		}
		if !second.SetupCompleted || second.DeviceIdentity != first.DeviceIdentity || second.HotspotSSID != first.HotspotSSID || second.HotspotPassword != "fpvpilot" {
			t.Fatal("setup lost identity or credentials")
		}
	}
}
func TestUpgradeNeverRenamesAndPasswordIsLiteral(t *testing.T) {
	m := fixture(t)
	if err := m.InitialiseDevice(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(m.Config)
	if err != nil || cfg.HotspotSSID != "Original" {
		t.Fatal("upgrade renamed appliance")
	}
	if err := m.Apply(Values{Password: "  secret pass  ", Country: "NZ", SetupCompleted: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(Values{SSID: "My second name"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(m.Config)
	if err != nil || cfg.HotspotPassword != "  secret pass  " || !cfg.SetupCompleted {
		t.Fatal("blank replacement or whitespace changed password")
	}
	b, _ := os.ReadFile(m.Hostapd)
	if !strings.Contains(string(b), "wpa_passphrase=  secret pass  \n") || !strings.Contains(string(b), "country_code=NZ") {
		t.Fatal("hotspot differs from application settings")
	}
}

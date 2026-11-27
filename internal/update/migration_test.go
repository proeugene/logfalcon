package update

import (
	"bytes"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These inputs come from actual repository tags, not reconstructed examples.
// They establish config-parser migration evidence, not an old-image upgrade.
func TestHistoricalConfigurationMigration(t *testing.T) {
	files, err := filepath.Glob("testdata/*.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if err = toml.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}
			i := installerFixture(t)
			path := i.dest("/etc/logfalcon/logfalcon.toml")
			if err = os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			host := "ssid=" + v["hotspot_ssid"].(string) + "\nwpa_passphrase=" + v["hotspot_password"].(string) + "\ncountry_code=GB\n"
			if err = os.WriteFile(i.dest("/etc/logfalcon/hostapd.conf"), []byte(host), 0600); err != nil {
				t.Fatal(err)
			}
			err = i.Run()
			// Early Go configs lack an explicit storage-pressure choice. They cannot be
			// safely defaulted by the upgrader and require the operator's decision.
			_, complete := v["storage_pressure_cleanup"]
			if !complete {
				if err == nil || !strings.Contains(err.Error(), "storage_pressure_cleanup") {
					t.Fatalf("incomplete config accepted: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(b, after) {
				t.Fatal("historical settings rewritten")
			}
		})
	}
}

package led

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLEDDiscoveryAndMissingPathDisablesWrites(t *testing.T) {
	root := t.TempDir()
	if got := discoverLED(root); got != "" {
		t.Fatal(got)
	}
	for _, name := range []string{"led0", "ACT"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, attr := range []string{"trigger", "brightness"} {
			if err := os.WriteFile(filepath.Join(dir, attr), []byte("0"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if got := discoverLED(root); got != dir {
			t.Fatalf("discovery %s", got)
		}
	}
	b := &sysfsBackend{dir: discoverLED(root)}
	if err := os.Remove(filepath.Join(b.dir, "brightness")); err != nil {
		t.Fatal(err)
	}
	b.Set(true)
	if b.dir != "" {
		t.Fatal("failed backend remains active")
	}
	for i := 0; i < 100; i++ {
		b.Set(true)
		b.RestoreTrigger()
	}
	if _, err := os.Stat(filepath.Join(root, "ACT", "brightness")); !os.IsNotExist(err) {
		t.Fatal("missing brightness recreated")
	}
}

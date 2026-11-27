package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Sparse fixtures exercise byte boundaries without allocating multi-GB images.
// They do not model compression ratios or validate a bootable image.
func TestImageSizeBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, phase string
		bytes       int64
		pass        bool
	}{
		{"reported failed image fits raw budget", "raw", 2263929344, true},
		{"raw boundary", "raw", 3072 * 1024 * 1024, true},
		{"raw regression", "raw", 3072*1024*1024 + 1, false},
		{"download boundary", "compressed", 600 * 1024 * 1024, true},
		{"download regression", "compressed", 600*1024*1024 + 1, false},
		{"raw bytes are not a download", "compressed", 2263929344, false},
		{"unknown phase", "other", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.img")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = f.Truncate(tc.bytes)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("fixture: %v %v", err, closeErr)
			}
			out, err := exec.Command("bash", "../../scripts/check-image-size.sh", tc.phase, path).CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("pass=%v, error=%v: %s", tc.pass, err, out)
			}
		})
	}
}

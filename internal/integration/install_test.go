package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellSyntax(t *testing.T) {
	for _, dir := range []string{"../../scripts", "../../system", "../../pi-gen/stage-logfalcon"} {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".sh") {
				return nil
			}
			if out, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
				t.Errorf("%s: %v %s", path, err, out)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallationAssetsStayConsistent(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	installer := read("../../scripts/install.sh")
	localInstaller := read("../../install.sh")
	image := read("../../pi-gen/stage-logfalcon/00-install-logfalcon/00-run.sh")
	for _, asset := range []string{"logfalcon@.service", "logfalcon-runtime.service", "logfalcon-ready-led.service", "logfalcon-diagnostics.service", "logfalcon-diagnostics-export.service", "logfalcon-diagnostics-export.timer", "wifi-init.sh", "99-betaflight-fc.rules"} {
		if !strings.Contains(installer, asset) || !strings.Contains(image, asset) || !strings.Contains(localInstaller, asset) {
			t.Errorf("asset absent from an installation path: %s", asset)
		}
		_ = read("../../system/" + asset)
	}
	if strings.Contains(installer, "cat > /etc/systemd/system/logfalcon") {
		t.Fatal("installer embeds divergent service units")
	}
}

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootLEDDiscoveryAndMissingPaths(t *testing.T) {
	script, err := os.ReadFile("../../system/logfalcon-boot-led.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ACT", "led0", "missing"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if name != "missing" {
				if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
					t.Fatal(err)
				}
				for _, attr := range []string{"trigger", "brightness"} {
					if err := os.WriteFile(filepath.Join(root, name, attr), []byte("0"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			body := strings.ReplaceAll(string(script), "/sys/class/leds", root)
			// Complete one pulse without sleeping in the fixture; remove the path
			// during the first wait to exercise disappearing hardware as well.
			body = strings.ReplaceAll(body, "sleep 1", `rm -f "$BRIGHTNESS" "$TRIGGER"; rmdir "$LED"`)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "sh", "-c", body).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if strings.Contains(string(out), "cannot create") || strings.Count(string(out), "unavailable") > 1 {
				t.Fatalf("repeated path errors: %s", out)
			}
		})
	}
}

func TestLEDServicesOrderHandover(t *testing.T) {
	boot, err := os.ReadFile("../../system/logfalcon-boot-led.service")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := os.ReadFile("../../system/logfalcon-ready-led.service")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(boot), "Before=sysinit.target") || !strings.Contains(string(ready), "After=sysinit.target ") {
		t.Fatal("boot startup must complete before the status service begins")
	}
	if !strings.Contains(string(ready), "ExecStartPre=/usr/bin/systemctl stop logfalcon-boot-led.service") || !strings.Contains(string(boot), "TimeoutStopSec=3s") || !strings.Contains(string(ready), "TimeoutStartSec=10s") {
		t.Fatal("handover must stop/reap boot with bounded waiting before starting status")
	}
	for _, line := range strings.Split(string(ready), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "logfalcon-boot-led.service") && !strings.HasPrefix(line, "ExecStartPre=") {
			t.Fatal("direct status/boot dependency can obstruct stop or restart boot", line)
		}
	}
	web, err := os.ReadFile("../../system/logfalcon-web.service")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{string(boot), string(ready), string(web)} {
		if strings.Contains(unit, "Conflicts=") {
			t.Fatal("wanted startup units must not conflict")
		}
	}
}

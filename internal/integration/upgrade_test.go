package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

// Run the local installer twice against a private filesystem with OS operations
// stubbed. The real file-copy/config-writing paths execute; this is not a Pi boot.
func TestUpgradePreservesOperatorSettings(t *testing.T) {

	root := t.TempDir()
	project := filepath.Join(root, "source")
	bin := filepath.Join(root, "tools")
	for _, dir := range []string{"system", "config", "boot"} {
		if err := os.CopyFS(filepath.Join(project, dir), os.DirFS("../../"+dir)); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, filepath.Join(project, "bin/logfalcon-amd64"), "#!/bin/sh\nexit 0\n")
	script, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(script), "if [[ $EUID -ne 0 ]]; then", "if false; then")
	s = strings.NewReplacer("$SCRIPT_DIR/boot/", "$SCRIPT_DIR/boot/", "/etc/", root+"/etc/", "/opt/", root+"/opt/", "/mnt/", root+"/mnt/", "/boot/", root+"/boot/", "/var/", root+"/var/", "/run/", root+"/run/", "/usr/share/logfalcon/", root+"/usr/share/logfalcon/").Replace(s)
	writeFixture(t, filepath.Join(project, "install.sh"), s)
	for _, command := range []string{"apt-get", "rfkill", "id", "usermod", "useradd", "systemctl", "udevadm", "chown", "flock"} {
		writeFixture(t, filepath.Join(bin, command), "#!/bin/sh\nexit 0\n")
	}
	writeFixture(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho x86_64\n")
	// Same substitutions on macOS; Linux CI executes the real GNU sed path.
	if runtime.GOOS == "darwin" {
		writeFixture(t, filepath.Join(bin, "sed"), "#!/bin/sh\nif [ \"$1\" = -i ]; then shift; exec /usr/bin/sed -i '' \"$@\"; fi\nexec /usr/bin/sed \"$@\"\n")
	}

	for _, dir := range []string{"/etc/dnsmasq.d", "/etc/udev/rules.d"} {
		if err := os.MkdirAll(root+dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, root+"/opt/logfalcon/logfalcon", "#!/bin/sh\nexit 0\n")
	writeFixture(t, root+"/etc/default/hostapd", "#DAEMON_CONF=\n")
	writeFixture(t, root+"/etc/avahi/avahi-daemon.conf", "#host-name=\n")
	files := map[string]string{
		"/etc/logfalcon/logfalcon.toml":           "hotspot_ssid = \"Pilot\"\nhotspot_password = \"privatepassword\"\ndebug_logging = false\nweb_port = 8080\n",
		"/etc/hostapd/hostapd.conf":               "ssid=Pilot\nwpa_passphrase=privatepassword\ncountry_code=DE\n",
		"/boot/firmware/logfalcon-config.txt":     "# Operator's existing boot input\nDEBUG=false\n",
		"/etc/wpa_supplicant/wpa_supplicant.conf": "country=DE\n",
	}
	for path, data := range files {
		writeFixture(t, root+path, data)
	}
	for i := 0; i < 2; i++ {
		cmd := exec.Command("bash", filepath.Join(project, "install.sh"))
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("upgrade %d: %v\n%s", i, err, out)
		}
	}
	for path, want := range files {
		got, err := os.ReadFile(root + path)
		if err != nil || string(got) != want {
			t.Fatalf("upgrade replaced %s: %v", path, err)
		}
	}
	got, err := os.ReadFile(root + "/etc/logfalcon/hostapd.conf")
	if err != nil || string(got) != files["/etc/hostapd/hostapd.conf"] {
		t.Fatal("legacy hotspot settings not migrated")
	}
}

func TestRemoteInstallerRejectsBadReleaseBeforeMutation(t *testing.T) {
	for _, mode := range []string{"missing", "checksum"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "tools")
			script, err := os.ReadFile("../../scripts/install.sh")
			if err != nil {
				t.Fatal(err)
			}
			s := strings.ReplaceAll(string(script), "[[ $EUID -eq 0 ]] || die", "true || die")
			writeFixture(t, filepath.Join(root, "install.sh"), s)
			writeFixture(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho aarch64\n")
			curl := "#!/bin/sh\nexit 22\n"
			if mode == "checksum" {
				curl = `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; output=$1; fi
  shift
done
if [ "$(basename "$output")" = checksums.txt ]; then
  for f in "$(dirname "$output")"/*; do printf '%064d  %s\n' 0 "$(basename "$f")"; done > "$output"
  printf '%064d  logfalcon-arm64\n' 0 >> "$output"
else
  printf 'corrupt payload' > "$output"
fi
`
			}
			writeFixture(t, filepath.Join(bin, "curl"), curl)
			if _, err := exec.LookPath("sha256sum"); err != nil {
				writeFixture(t, filepath.Join(bin, "sha256sum"), "#!/bin/sh\nprintf '%064d  fixture\\n' 1\n")
			}
			writeFixture(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\nexit 99\n")
			marker := filepath.Join(root, "mutated")
			writeFixture(t, filepath.Join(bin, "apt-get"), "#!/bin/sh\ntouch '"+marker+"'\nexit 99\n")
			cmd := exec.Command("bash", filepath.Join(root, "install.sh"))
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "LOGFALCON_VERSION=v0.0.0-old")
			out, err := cmd.CombinedOutput()
			want := "no system changes made"
			if mode == "checksum" {
				want = "Checksum mismatch"
			}
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("bad release not explained: %v %s", err, out)
			}
			if _, err = os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("installer mutated system before checking assets")
			}
		})
	}
}

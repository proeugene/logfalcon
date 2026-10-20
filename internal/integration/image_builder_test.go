package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func builderHelpers(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../pi-gen/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(b), "pigen_capsh() {")
	end := strings.Index(string(b), "\ncontainer_main() {")
	if start < 0 || end <= start {
		t.Fatal("builder helpers missing")
	}
	return string(b[start:end])
}

func TestPiGenBootstrapFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"scripts", "stage0", "export-image/05-finalise"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Match the pinned upstream calls, including its masked bootstrap failure.
	common := `bootstrap() {
    local BOOTSTRAP_STR=unused
    setarch linux32 capsh $CAPSH_ARG -- -c "'${BOOTSTRAP_CMD}' $BOOTSTRAP_STR" || true
}
on_chroot() {
    setarch linux32 capsh $CAPSH_ARG "--chroot=${ROOTFS_DIR}/" -- -e "$@"
}
`
	if err := os.WriteFile(filepath.Join(dir, "scripts", "common"), []byte(common), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-e", "-c", builderHelpers(t)+`
prepare_pigen "$1"
source "$1/scripts/common"
pigen_capsh() { return 42; }
bootstrap
echo INCORRECTLY_CONTINUED
`, "test", dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 || bytes.Contains(out, []byte("INCORRECTLY_CONTINUED")) {
		t.Fatalf("bootstrap failure swallowed: %v %s", err, out)
	}
}

func TestPiGenCapshPersonalitySelection(t *testing.T) {
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"setarch", "capsh"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\nprintf '%s\\n' \"${0##*/}\" \"$@\"\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-e", "-c", builderHelpers(t)+"\npigen_capsh --chroot=/probe -- -e")
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "LF_USE_LINUX32="+mode)
			out, err := cmd.CombinedOutput()
			want := "capsh\n--chroot=/probe\n--\n-e\n"
			if mode == "1" {
				want = "setarch\nlinux32\n" + want
			}
			if err != nil || string(out) != want {
				t.Fatalf("personality selection: %v %q, want %q", err, out, want)
			}
		})
	}
}

func TestPiGenDownloadOptionsRemovedBeforeExport(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"scripts", "stage0", "export-image/05-finalise", "rootfs/etc/apt/apt.conf.d"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts/common"), []byte("setarch linux32 capsh\nsetarch linux32 capsh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-e", "-c", builderHelpers(t)+`
prepare_pigen "$1"
export ROOTFS_DIR="$1/rootfs"
bash -e "$1/stage0/prerun.sh"
cat "$ROOTFS_DIR/etc/apt/apt.conf.d/99-logfalcon-build-downloads"
bash -e "$1/export-image/05-finalise/01-run.sh"
test ! -e "$ROOTFS_DIR/etc/apt/apt.conf.d/99-logfalcon-build-downloads"
`, "test", dir)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte(`Acquire::http::Pipeline-Depth "0";`)) || !bytes.Contains(out, []byte(`Acquire::Retries "3";`)) {
		t.Fatalf("temporary download options: %v %s", err, out)
	}
}

func TestImageBuilderHelpDoesNotRequireDocker(t *testing.T) {
	out, err := exec.Command("bash", "../../pi-gen/build.sh", "--help").CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("--check")) || !bytes.Contains(out, []byte("pi-gen/deploy")) {
		t.Fatalf("help: %v %s", err, out)
	}
}

func TestNativeImageInspectionRequiresContainer(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/inspect-image.sh", "--native", "/not-an-image")
	cmd.Env = append(os.Environ(), "LOGFALCON_IMAGE_CONTAINER=0")
	out, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("reserved for the Linux image-build container")) {
		t.Fatalf("native guard: %v %s", err, out)
	}
}

func TestImageInspectionRedactsCredentials(t *testing.T) {
	b, err := os.ReadFile("../../scripts/inspect-image.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	start := strings.Index(script, "redact_config() {")
	if start < 0 {
		t.Fatal("redactor missing")
	}
	end := strings.Index(script[start:], "\n}\n")
	if end < 0 {
		t.Fatal("redactor unterminated")
	}
	redactor := script[start : start+end+3]
	cmd := exec.Command("bash", "-c", redactor+"\nredact_config")
	cmd.Stdin = strings.NewReader("SSID=Visible\nPASSWORD=secret-sentinel\n  hotspot_password = \"secret-sentinel\"\nwpa_passphrase=secret-sentinel\nwpa_psk=secret-sentinel\n")
	out, err := cmd.CombinedOutput()
	if err != nil || bytes.Contains(out, []byte("secret-sentinel")) || !bytes.Contains(out, []byte("SSID=Visible")) || bytes.Count(out, []byte("[redacted]")) != 4 {
		t.Fatalf("credentials leaked or unrelated data lost: %v %s", err, out)
	}
}

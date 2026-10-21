package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	lfsync "github.com/proeugene/logfalcon/internal/sync"
)

var executableOnce sync.Once
var executable []byte
var executableErr error

func testExecutable(t *testing.T) []byte {
	t.Helper()
	executableOnce.Do(func() {
		dir, err := os.MkdirTemp("", "logfalcon-update-test-")
		if err != nil {
			executableErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		source := filepath.Join(dir, "main.go")
		code := "package main\nimport \"fmt\"\nvar Version,BuildCommit string\nfunc main(){fmt.Println(Version,BuildCommit)}\n"
		if err = os.WriteFile(source, []byte(code), 0600); err != nil {
			executableErr = err
			return
		}
		cmd := exec.Command("go", "build", "-ldflags", "-X main.Version=v0.5.0 -X main.BuildCommit="+strings.Repeat("a", 40), "-o", filepath.Join(dir, "app"), source)
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm", "GOARM=6", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			executableErr = errors.New(string(out))
			return
		}
		executable, executableErr = os.ReadFile(filepath.Join(dir, "app"))
	})
	if executableErr != nil {
		t.Fatal(executableErr)
	}
	return executable
}
func testPackage(t *testing.T) ([]byte, map[string]ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for n := range Destinations {
		files[n] = []byte("new " + n)
	}
	files["bin/logfalcon"] = testExecutable(t)
	path := filepath.Join(t.TempDir(), "release.lfu")
	if err = Build(path, Manifest{Format: 1, Version: "v0.5.0", Commit: strings.Repeat("a", 40), Architecture: "arm6", Base: Base, KeyID: "test", Migration: 1}, files, key); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b, map[string]ed25519.PublicKey{"test": pub}, key
}
func TestSignedPackages(t *testing.T) {
	b, keys, _ := testPackage(t)
	verify := func(b []byte, arch, current string) error {
		_, err := Verify(bytes.NewReader(b), int64(len(b)), keys, arch, current)
		return err
	}
	if err := verify(b, "arm6", "v0.4.6"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ arch, current string }{{"arm64", "v0.4.6"}, {"arm6", "v0.5.0"}, {"arm6", "v0.6.0"}, {"arm6", "dev"}} {
		if verify(b, tt.arch, tt.current) == nil {
			t.Fatalf("incompatible accepted: %+v", tt)
		}
	}
	if _, err := Verify(bytes.NewReader(b), int64(len(b)), map[string]ed25519.PublicKey{}, "arm6", "v0.4.6"); err == nil {
		t.Fatal("untrusted key accepted")
	}
	// Rebuild ZIP CRCs after tampering, so signature/hash verification must reject.
	for _, entry := range []string{"manifest.json", "manifest.sig", "bin/logfalcon"} {
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		for _, f := range z.File {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			data, e := io.ReadAll(r)
			if e != nil {
				t.Fatal(e)
			}
			_ = r.Close()
			if f.Name == entry {
				data[0] ^= 1
			}
			w, e := zw.Create(f.Name)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = w.Write(data)
		}
		_ = zw.Close()
		if verify(out.Bytes(), "arm6", "v0.4.6") == nil {
			t.Fatalf("tampering accepted: %s", entry)
		}
	}
	for _, name := range []string{"../escape", "/etc/passwd", "system/evil.sh", "keys/../key.pub"} {
		var out bytes.Buffer
		z := zip.NewWriter(&out)
		w, _ := z.Create(name)
		_, _ = w.Write([]byte("bad"))
		_ = z.Close()
		if verify(out.Bytes(), "arm6", "v0.4.6") == nil {
			t.Fatal("unsafe entry accepted")
		}
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	h := &zip.FileHeader{Name: "bin/logfalcon"}
	h.SetMode(os.ModeSymlink | 0777)
	w, _ := z.CreateHeader(h)
	_, _ = w.Write([]byte("/etc/passwd"))
	_ = z.Close()
	if verify(out.Bytes(), "arm6", "v0.4.6") == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := Verify(bytes.NewReader(b), MaxUpload+1, keys, "arm6", "v0.4.6"); err == nil {
		t.Fatal("oversize accepted")
	}
}
func installerFixture(t *testing.T) Installer {
	t.Helper()
	b, keys, _ := testPackage(t)
	root := t.TempDir()
	i := Installer{Root: root, StateDir: filepath.Join(root, "state"), RuntimeDir: filepath.Join(root, "run"), Inbox: filepath.Join(root, "inbox"), Keys: filepath.Join(root, "keys"), Current: "v0.4.6", Architecture: "arm6", Healthy: func(Manifest) error { return nil }}
	for _, dir := range []string{i.StateDir, i.RuntimeDir, i.Inbox, i.Keys, filepath.Join(root, "etc/logfalcon"), filepath.Join(root, "opt/logfalcon")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, key := range keys {
		if err := os.WriteFile(filepath.Join(i.Keys, name+".pub"), []byte(hex.EncodeToString(key)), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Deliberately enabled destructive values must survive byte-for-byte.
	app := "erase_after_sync = true\nstorage_pressure_cleanup = true\nflash_read_compression = false\nhotspot_ssid = \"Custom\"\nhotspot_password = \"spaces ok  \"\nstorage_path = \"/mnt/logfalcon-logs\"\ndebug_logging = false\n"
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "etc/logfalcon/logfalcon.toml"), []byte(app))
	write(filepath.Join(root, "etc/logfalcon/hostapd.conf"), []byte("ssid=Custom\nwpa_passphrase=spaces ok  \ncountry_code=NZ\n"))
	write(filepath.Join(root, "opt/logfalcon/logfalcon"), []byte("previous binary"))
	write(filepath.Join(root, "opt/logfalcon/updater"), []byte("previous helper"))
	write(filepath.Join(i.Inbox, "package.lfu"), b)
	write(filepath.Join(i.Inbox, "request"), []byte("install"))
	return i
}
func TestInstallAndRollback(t *testing.T) {
	for _, failure := range []string{"", "space", "health", "restart", "copy", "interruption"} {
		t.Run(failure, func(t *testing.T) {
			i := installerFixture(t)
			app := i.dest("/etc/logfalcon/logfalcon.toml")
			before, _ := os.ReadFile(app)
			if failure == "space" {
				i.Space = func(int64) error { return errors.New("no space") }
			}
			if failure == "health" {
				i.Healthy = func(Manifest) error { return errors.New("failed startup") }
			}
			restarts := 0
			i.Restart = func() error {
				restarts++
				if failure == "restart" && restarts == 1 {
					return errors.New("restart failure")
				}
				return nil
			}
			if failure == "copy" {
				lock, err := lfsync.AcquireTransferLock(i.RuntimeDir)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
			}
			if failure == "interruption" {
				// Simulate process loss after the new executable was published.
				old := []byte("previous binary")
				_ = replace(filepath.Join(i.StateDir, "previous-0"), old, 0600)
				_ = replace(i.dest("/opt/logfalcon/logfalcon"), []byte("broken"), 0755)
				if err := i.save(State{Phase: "activating", Files: []string{"/opt/logfalcon/logfalcon"}}); err != nil {
					t.Fatal(err)
				}
			}
			err := i.Run()
			if failure == "" || failure == "interruption" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("failure reported as success")
			}
			after, _ := os.ReadFile(app)
			if !bytes.Equal(before, after) {
				t.Fatal("configuration changed")
			}
			binary, _ := os.ReadFile(i.dest("/opt/logfalcon/logfalcon"))
			state := ReadState(i.StateDir)
			if failure == "" {
				if state.Phase != "success" || !bytes.Equal(binary, testExecutable(t)) {
					t.Fatalf("state=%+v binary=%s", state, binary)
				}
			} else {
				if string(binary) != "previous binary" {
					t.Fatalf("previous executable not restored: %s", binary)
				}
				if (failure == "health" || failure == "restart" || failure == "interruption") && state.Phase != "rolled-back" {
					t.Fatalf("rollback not recorded: %+v", state)
				}
			}
		})
	}
}
func TestIncompleteUpgradeRejected(t *testing.T) {
	i := installerFixture(t)
	path := i.dest("/etc/logfalcon/logfalcon.toml")
	b, _ := os.ReadFile(path)
	b = bytes.ReplaceAll(b, []byte("erase_after_sync = true\n"), nil)
	_ = os.WriteFile(path, b, 0600)
	if err := i.Run(); err == nil {
		t.Fatal("missing destructive setting defaulted")
	}
	binary, _ := os.ReadFile(i.dest("/opt/logfalcon/logfalcon"))
	if string(binary) != "previous binary" {
		t.Fatal("changed installation")
	}
}

func TestCumulativeKeyRotation(t *testing.T) {
	b, oldKeys, oldKey := testPackage(t)
	pkg, err := Verify(bytes.NewReader(b), int64(len(b)), oldKeys, "arm6", "v0.4.6")
	if err != nil {
		t.Fatal(err)
	}
	pub, next, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubhex := hex.EncodeToString(pub)
	pkg.Manifest.KeyID = "next"
	pkg.Manifest.Certificates = []Certificate{{ID: "next", PublicKey: pubhex, SignedBy: "test", Signature: hex.EncodeToString(ed25519.Sign(oldKey, CertificateMessage("next", pubhex)))}}
	pkg.Files["keys/next.pub"] = []byte(pubhex + "\n")
	path := filepath.Join(t.TempDir(), "rotated.lfu")
	if err = Build(path, pkg.Manifest, pkg.Files, next); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// An appliance that skipped the rotation release still trusts the new key
	// through the certificate signed by its original provisioned key.
	if _, err = Verify(bytes.NewReader(b), int64(len(b)), oldKeys, "arm6", "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	pkg.Manifest.Certificates[0].Signature = strings.Repeat("00", 64)
	if err = Build(path, pkg.Manifest, pkg.Files, next); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(bytes.NewReader(b), int64(len(b)), oldKeys, "arm6", "v0.2.0"); err == nil {
		t.Fatal("forged delegation accepted")
	}
}
func TestArchiveBoundsAndDuplicates(t *testing.T) {
	_, keys, _ := testPackage(t)
	for _, duplicate := range []bool{false, true} {
		var out bytes.Buffer
		z := zip.NewWriter(&out)
		if duplicate {
			for range 2 {
				w, _ := z.Create("manifest.json")
				_, _ = w.Write([]byte("{}"))
			}
		} else {
			w, _ := z.Create("bin/logfalcon")
			block := make([]byte, 1<<20)
			for range 65 {
				_, _ = w.Write(block)
			}
		}
		_ = z.Close()
		if _, err := Verify(bytes.NewReader(out.Bytes()), int64(out.Len()), keys, "arm6", "v0.4.6"); err == nil {
			t.Fatal("archive bounds or duplicate check bypassed")
		}
	}
}

func TestSignedBinaryIdentityMismatch(t *testing.T) {
	b, keys, key := testPackage(t)
	pkg, err := Verify(bytes.NewReader(b), int64(len(b)), keys, "arm6", "v0.4.6")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"version", "commit", "architecture"} {
		t.Run(kind, func(t *testing.T) {
			m := pkg.Manifest
			arch := "arm6"
			switch kind {
			case "version":
				m.Version = "v0.6.0"
			case "commit":
				m.Commit = strings.Repeat("b", 40)
			case "architecture":
				m.Architecture = "arm64"
				arch = "arm64"
			}
			path := filepath.Join(t.TempDir(), "mismatch.lfu")
			if err := Build(path, m, pkg.Files, key); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(bytes.NewReader(content), int64(len(content)), keys, arch, "v0.4.6"); err == nil {
				t.Fatal("signed metadata and executable identity mismatch accepted")
			}
		})
	}
}
func TestBusyRecoveryNeverOverwritesJournal(t *testing.T) {
	i := installerFixture(t)
	state := State{Phase: "activating", Version: "v0.5.0", Files: []string{"/opt/logfalcon/logfalcon"}}
	if err := i.save(state); err != nil {
		t.Fatal(err)
	}
	lock, err := lfsync.AcquireTransferLock(i.RuntimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err = i.Run(); err == nil {
		t.Fatal("busy recovery proceeded")
	}
	saved := ReadState(i.StateDir)
	if saved.Phase != "activating" || len(saved.Files) != 1 {
		t.Fatal("busy retry destroyed recovery journal")
	}
}

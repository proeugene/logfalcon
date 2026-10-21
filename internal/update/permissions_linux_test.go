package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Run the real worker with the service's bounding set, rather than an
// unrestricted root test process. This helper also verifies inbox cleanup.
func TestWorkerPermissionsHelper(t *testing.T) {
	dir := os.Getenv("LOGFALCON_PERMISSION_FIXTURE")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	syscall.Umask(0077)
	i := Installer{Root: dir, RuntimeDir: filepath.Join(dir, "runtime"), StateDir: filepath.Join(dir, "state"), Inbox: filepath.Join(dir, "inbox")}
	err := i.Run()
	if os.Getenv("LOGFALCON_PERMISSION_MODE") == "idle" {
		if err != nil {
			t.Fatal(err)
		}
	} else if os.Getenv("LOGFALCON_PERMISSION_MODE") == "busy" {
		if err == nil {
			t.Fatal("worker ignored held transfer lock")
		}
	} else {
		if err == nil {
			t.Fatal("invalid upload accepted")
		}
		if _, err := os.Stat(filepath.Join(i.Inbox, "request")); !os.IsNotExist(err) {
			t.Fatal("web-owned request not removed:", err)
		}
	}
}

func TestWorkerAndServiceAccountShareLock(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires Linux root; run in installer integration container")
	}
	for _, cmd := range []string{"setpriv", "flock"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Fatal(err)
		}
	}
	service, err := os.ReadFile("../../system/logfalcon-update.service")
	if err != nil {
		t.Fatal(err)
	}
	caps := ""
	for line := range strings.SplitSeq(string(service), "\n") {
		if value, ok := strings.CutPrefix(line, "CapabilityBoundingSet="); ok {
			for _, cap := range strings.Fields(value) {
				caps += ",+" + strings.ToLower(strings.TrimPrefix(cap, "CAP_"))
			}
		}
	}
	if caps == "" {
		t.Fatal("missing service bounding set")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, first := range []string{"worker", "service"} {
		t.Run(first, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "logfalcon-permissions-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"runtime", "inbox"} {
				path := filepath.Join(dir, name)
				if err := os.Mkdir(path, 0770); err != nil {
					t.Fatal(err)
				}
				mode := os.FileMode(0770)
				if name == "runtime" {
					mode = 0755
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chown(path, 65534, 65534); err != nil {
					t.Fatal(err)
				}
			}
			lockPath := filepath.Join(dir, "runtime", "transfer.lock")
			userLock := func(wantBusy bool) {
				cmd := exec.Command("setpriv", "--reuid=65534", "--regid=65534", "--clear-groups", "flock", "-n", lockPath, "true")
				out, err := cmd.CombinedOutput()
				if wantBusy {
					if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
						t.Fatalf("expected lock contention: %v %s", err, out)
					}
				} else if err != nil {
					t.Fatalf("service cannot acquire lock: %v %s", err, out)
				}
			}
			worker := func(mode string) {
				cmd := exec.Command("setpriv", "--bounding-set=-all"+caps, exe, "-test.run=^TestWorkerPermissionsHelper$")
				cmd.Env = append(os.Environ(), "LOGFALCON_PERMISSION_FIXTURE="+dir, "LOGFALCON_PERMISSION_MODE="+mode)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("bounded worker: %v %s", err, out)
				}
			}
			if first == "service" {
				userLock(false)
			}
			worker("idle")
			before, err := os.Stat(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			stat := before.Sys().(*syscall.Stat_t)
			if stat.Uid != 65534 || stat.Gid != 65534 || before.Mode().Perm() != 0644 {
				t.Fatalf("lock has wrong ownership/mode: %+v %v", stat, before.Mode())
			}
			userLock(false)
			f, err := os.OpenFile(lockPath, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			worker("busy")
			userLock(true)
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
				t.Fatal(err)
			}
			request := filepath.Join(dir, "inbox", "request")
			if err := os.WriteFile(request, []byte("invalid test upload"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(request, 65534, 65534); err != nil {
				t.Fatal(err)
			}
			worker("invalid")
			after, err := os.Stat(lockPath)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("worker replaced shared lock inode:", err)
			}
			userLock(false)
		})
	}
}

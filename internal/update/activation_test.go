package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lfsync "github.com/proeugene/logfalcon/internal/sync"
)

const canonicalPolicy = "/etc/polkit-1/rules.d/50-logfalcon-hostapd.rules"
const obsoletePolicy = "/etc/polkit-1/rules.d/50-logfalcon.rules"

func TestPolicyMigrationAndRollback(t *testing.T) {
	for _, previous := range []string{"fresh-install", "old-updater", "both"} {
		for _, rollback := range []bool{false, true} {
			t.Run(previous+map[bool]string{false: "/install", true: "/rollback"}[rollback], func(t *testing.T) {
				i := installerFixture(t)
				if err := os.MkdirAll(i.dest(filepath.Dir(canonicalPolicy)), 0755); err != nil {
					t.Fatal(err)
				}
				before := map[string][]byte{}
				for _, path := range []string{canonicalPolicy, obsoletePolicy} {
					if previous == "both" || (previous == "fresh-install" && path == canonicalPolicy) || (previous == "old-updater" && path == obsoletePolicy) {
						before[path] = []byte("previous policy at " + path)
						if err := os.WriteFile(i.dest(path), before[path], 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				i.Healthy = func(Manifest) error {
					if _, err := os.Stat(i.dest(obsoletePolicy)); !os.IsNotExist(err) {
						t.Error("obsolete policy still grants permissions during activation:", err)
					}
					if rollback {
						return errors.New("new application failed health check")
					}
					return nil
				}
				err := i.Run()
				if (err != nil) != rollback {
					t.Fatalf("Run() = %v, rollback=%v", err, rollback)
				}
				for _, path := range []string{canonicalPolicy, obsoletePolicy} {
					want := before[path]
					if !rollback {
						want = nil
						if path == canonicalPolicy {
							want = []byte("new system/50-logfalcon.rules")
						}
					}
					got, err := os.ReadFile(i.dest(path))
					if want == nil {
						if !os.IsNotExist(err) {
							t.Errorf("%s must be absent, got %q, %v", path, got, err)
						}
					} else if err != nil || string(got) != string(want) {
						t.Errorf("%s = %q, %v; want %q", path, got, err, want)
					}
				}
			})
		}
	}
}

func TestSystemActivationRefreshesLiveIntegration(t *testing.T) {
	for _, fail := range []string{"", "udevadm control --reload-rules", "systemctl try-restart systemd-journald.service", "systemctl try-restart logfalcon-ready-led.service"} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "commands")
			stub := "#!/bin/sh\ncommand=\"${0##*/} $*\"\nprintf '%s\\n' \"$command\" >> \"$LOGFALCON_ACTIVATION_LOG\"\n[ \"$command\" != \"$LOGFALCON_ACTIVATION_FAIL\" ]\n"
			for _, command := range []string{"systemctl", "udevadm"} {
				if err := os.WriteFile(filepath.Join(dir, command), []byte(stub), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir)
			t.Setenv("LOGFALCON_ACTIVATION_LOG", log)
			t.Setenv("LOGFALCON_ACTIVATION_FAIL", fail)
			err := System("v0.4.6").Restart()
			if (err != nil) != (fail != "") {
				t.Fatalf("activation error=%v, failed command=%q", err, fail)
			}
			b, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"systemctl daemon-reload",
				"udevadm control --reload-rules",
				"systemctl try-restart systemd-journald.service",
				"systemctl try-restart logfalcon-ready-led.service",
				"systemctl restart logfalcon-web.service",
			}
			for n, command := range want {
				if command == fail {
					want = want[:n+1]
					break
				}
			}
			if string(b) != strings.Join(want, "\n")+"\n" {
				t.Fatalf("activation commands:\n%s\nwant:\n%s", b, strings.Join(want, "\n"))
			}
		})
	}
}

func TestPolicyMigrationRecoversAfterInterruption(t *testing.T) {
	i := installerFixture(t)
	if err := os.MkdirAll(i.dest(filepath.Dir(canonicalPolicy)), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{canonicalPolicy, obsoletePolicy} {
		if err := os.WriteFile(i.dest(path), []byte("previous "+path), 0644); err != nil {
			t.Fatal(err)
		}
	}
	interrupted := errors.New("simulated worker loss")
	i.Healthy = func(Manifest) error { panic(interrupted) }
	func() {
		defer func() {
			if got := recover(); got != interrupted {
				t.Fatalf("expected worker interruption, got %v", got)
			}
		}()
		_ = i.Run()
	}()
	if ReadState(i.StateDir).Phase != "restarting" {
		t.Fatal("interruption lost recovery journal")
	}
	i.Healthy = nil
	if err := i.Run(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{canonicalPolicy, obsoletePolicy} {
		b, err := os.ReadFile(i.dest(path))
		if err != nil || string(b) != "previous "+path {
			t.Errorf("interrupted migration did not restore %s: %q, %v", path, b, err)
		}
	}
	if state := ReadState(i.StateDir); state.Phase != "rolled-back" {
		t.Fatalf("recovery state: %+v", state)
	}
}

func TestRebootReminderRequiresNewBootAndHealthyApplication(t *testing.T) {
	i := installerFixture(t)
	boot := "first-boot"
	i.BootID = func() string { return boot }
	if err := i.Run(); err != nil {
		t.Fatal(err)
	}
	state := ReadState(i.StateDir)
	if !state.RebootRequired || state.ActivatedBootID != boot || !strings.Contains(state.Message, "Reboot") {
		t.Fatalf("update did not expose reboot requirement: %+v", state)
	}
	for _, id := range []string{"first-boot", ""} {
		boot = id
		if err := i.Run(); err != nil {
			t.Fatal(err)
		}
		if !ReadState(i.StateDir).RebootRequired {
			t.Fatal("same or unknown boot cleared reboot requirement")
		}
	}
	boot = "second-boot"
	i.Healthy = func(Manifest) error { return errors.New("web unavailable") }
	if err := i.Run(); err == nil || !ReadState(i.StateDir).RebootRequired {
		t.Fatal("failed post-reboot health confirmation cleared reminder:", err)
	}
	i.Healthy = func(m Manifest) error {
		if m.Version != state.Version || m.Commit != state.Commit {
			t.Fatalf("post-reboot check lost release identity: %+v", m)
		}
		return nil
	}
	if err := i.Run(); err != nil {
		t.Fatal(err)
	}
	if state := ReadState(i.StateDir); state.RebootRequired || state.Phase != "success" || !strings.Contains(state.Message, "Reboot completed") {
		t.Fatalf("reboot not confirmed: %+v", state)
	}
}

func TestBootWorkerLeavesCollectionLockAvailable(t *testing.T) {
	i := installerFixture(t)
	if err := os.Remove(filepath.Join(i.Inbox, "request")); err != nil {
		t.Fatal(err)
	}
	lock, err := lfsync.AcquireTransferLock(i.RuntimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Run(); err != nil {
		t.Errorf("idle worker collided with a collection: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := i.save(State{Phase: "success", Version: "v0.5.0", RebootRequired: true, ActivatedBootID: "old"}); err != nil {
		t.Fatal(err)
	}
	i.BootID = func() string { return "new" }
	i.Healthy = func(Manifest) error {
		// Represents a controller attached while the web service is starting.
		collection, err := lfsync.AcquireTransferLock(i.RuntimeDir)
		if err != nil {
			t.Fatalf("boot health poll prevented a collection from starting: %v", err)
		}
		t.Cleanup(func() { _ = collection.Close() })
		return nil
	}
	if err := i.Run(); err != nil || ReadState(i.StateDir).RebootRequired {
		t.Fatalf("post-reboot health confirmation: %v", err)
	}
}

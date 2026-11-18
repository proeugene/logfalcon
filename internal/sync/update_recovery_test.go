package sync

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUpdateRecoveryCheckedUnderTransferLock(t *testing.T) {
	for _, state := range []string{`{"Phase":"activating"}`, `{"Phase":"restarting"}`, `{"Phase":"unknown"}`, `{broken`} {
		t.Run(state, func(t *testing.T) {
			o, peer := transferFixture(t, 1, "")
			checked := false
			o.readUpdateState = func(path string) ([]byte, error) {
				checked = true
				lock, err := acquireTransferLock(filepath.Dir(o.Config.StatusPath))
				if err == nil {
					_ = lock.Close()
					t.Fatal("recovery inspected before excluding the updater")
				}
				return []byte(state), nil
			}
			if got := o.Run("fake"); got != ResultError || !checked || peer.requests != 0 {
				t.Fatal("pending recovery allowed serial access")
			}
			if _, err := os.Stat(o.Config.StatusPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("pending recovery published a transfer")
			}
			lock, err := acquireTransferLock(filepath.Dir(o.Config.StatusPath))
			if err != nil {
				t.Fatal("blocked recovery retained lock:", err)
			}
			_ = lock.Close()
		})
	}
}

func TestTransferLockRejectsSymlinkAndNonRegularFile(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "transfer.lock")
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			} else if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			lock, err := acquireTransferLock(dir)
			if err == nil {
				_ = lock.Close()
				t.Fatal("unsafe shared lock accepted")
			}
		})
	}
}

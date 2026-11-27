package sync

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUpdateRecoveryCheckedUnderTransferLock(t *testing.T) {
	for _, state := range []string{`{"Phase":"activating"}`, `{"Phase":"restarting"}`, `{"Phase":"unknown"}`, `{broken`} {
		t.Run(state, func(t *testing.T) {
			o, peer := transferFixture(t, 1, "")
			if err := BeginTransfer("previous"); err != nil {
				t.Fatal(err)
			}
			if err := setSavedCopy(SavedCopy{SessionID: "controller/previous", Bytes: 1}); err != nil {
				t.Fatal(err)
			}
			SetStatus("complete", 100, "Previous copy saved.")
			previousID := GetStatus().TransferID
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
			if s := GetStatus(); s.State != "error" || s.ReasonCode != "update_recovery" || s.TransferID == previousID || s.SavedCopy != nil {
				t.Errorf("blocked connection displayed stale or ambiguous status: %+v", s)
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

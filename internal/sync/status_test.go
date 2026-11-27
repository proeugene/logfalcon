package sync

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStatusCrossProcess(t *testing.T) {
	if path := os.Getenv("LOGFALCON_TEST_STATUS"); path != "" {
		ConfigureStatus(path)
		if err := BeginTransfer("fake"); err != nil {
			t.Fatal(err)
		}
		SetStatus("complete", 100, "Copy saved. Erase disabled.")
		return
	}
	path := filepath.Join(t.TempDir(), "status.json")
	child := exec.Command(os.Args[0], "-test.run=^TestStatusCrossProcess$")
	child.Env = append(os.Environ(), "LOGFALCON_TEST_STATUS="+path)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	s := ReadStatus(path)
	if s.State != "complete" || s.TransferID == "" || s.BootID == "" || s.UpdatedUTC.IsZero() {
		t.Fatalf("shared result not read: %+v", s)
	}
	// The writer has exited; a completed result must still remain visible.
}

func TestReadStatusRejectsDamagedCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	ConfigureStatus(path)
	t.Cleanup(func() { ConfigureStatus("") })
	if err := BeginTransfer("fake"); err != nil {
		t.Fatal(err)
	}
	SetStatus("complete", 100, "Saved.")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	missingIdentity, err := json.Marshal(Status{State: "complete", BootID: bootID()})
	if err != nil {
		t.Fatal(err)
	}
	for _, contents := range [][]byte{
		append(bytes.Clone(good), []byte("broken")...),
		append(bytes.Clone(good), bytes.Repeat([]byte(" "), 16384)...),
		missingIdentity,
	} {
		if err := os.WriteFile(path, contents, 0644); err != nil {
			t.Fatal(err)
		}
		if got := ReadStatus(path); got.State != "unavailable" {
			t.Errorf("damaged completion returned %q", got.State)
		}
	}
}

func TestStatusNeverClaimsInterruptedCopyCompleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	for _, tc := range []struct {
		status Status
		want   string
	}{
		{Status{State: "syncing", BootID: bootID(), PID: 0, UpdatedUTC: time.Now()}, "interrupted"},
		{Status{State: "complete", BootID: "another-boot"}, "waiting"},
	} {
		b, _ := json.Marshal(tc.status)
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
		if got := ReadStatus(path); got.State != tc.want {
			t.Fatal(got)
		}
	}
	if err := os.WriteFile(path, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if ReadStatus(path).State != "unavailable" {
		t.Fatal("corrupt status appeared healthy")
	}
}

func TestNewTransferClearsSavedCopyAndFailure(t *testing.T) {
	ConfigureStatus(filepath.Join(t.TempDir(), "status.json"))
	t.Cleanup(func() { ConfigureStatus("") })
	if err := BeginTransfer("first"); err != nil {
		t.Fatal(err)
	}
	if err := setSavedCopy(SavedCopy{SessionID: "controller/session", Bytes: 123, EraseAttempted: true}); err != nil {
		t.Fatal(err)
	}
	setFailure("erase_unconfirmed", "Copy saved; erase unconfirmed.")
	if err := BeginTransfer("next"); err != nil {
		t.Fatal(err)
	}
	status := GetStatus()
	if status.SavedCopy != nil || status.ReasonCode != "" || status.BytesCopied != 0 || status.State != "identifying" {
		t.Fatalf("new transfer inherited old result: %+v", status)
	}
}

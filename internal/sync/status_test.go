package sync

import (
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

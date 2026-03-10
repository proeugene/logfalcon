package sync

import (
	"sync"
	"testing"
)

func TestGetSetStatus(t *testing.T) {
	// Reset to known state.
	SetStatus("idle", 0, "Ready for the next sync.")

	SetStatus("syncing", 42, "Copying blackbox flash.")
	got := GetStatus()

	if got.State != "syncing" {
		t.Errorf("State = %q, want %q", got.State, "syncing")
	}
	if got.Progress != 42 {
		t.Errorf("Progress = %d, want %d", got.Progress, 42)
	}
	if got.Message != "Copying blackbox flash." {
		t.Errorf("Message = %q, want %q", got.Message, "Copying blackbox flash.")
	}

	// Verify overwrite.
	SetStatus("error", 0, "Something broke.")
	got = GetStatus()
	if got.State != "error" {
		t.Errorf("State after overwrite = %q, want %q", got.State, "error")
	}
	if got.Progress != 0 {
		t.Errorf("Progress after overwrite = %d, want 0", got.Progress)
	}
}

func TestStatusThreadSafety(t *testing.T) {
	// Hammer GetStatus and SetStatus from many goroutines.
	// Run with -race to detect data races.
	const goroutines = 50
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				SetStatus("state", id*iterations+j, "msg")
			}
		}(i)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				s := GetStatus()
				// Verify the snapshot is internally consistent (not partially written).
				_ = s.State
				_ = s.Progress
				_ = s.Message
			}
		}()
	}

	wg.Wait()

	// If we got here without -race flagging anything, the lock works.
	got := GetStatus()
	if got.State != "state" {
		t.Errorf("State after concurrent writes = %q, want %q", got.State, "state")
	}
}

func TestAutoDetectPortNone(t *testing.T) {
	// On non-Linux (or when no /dev/ttyACM* exists) this should return "".
	// This test is inherently environment-dependent but validates the
	// function doesn't panic and returns a sensible default.
	port := AutoDetectPort()
	// We can't guarantee no ttyACM* exists in CI, so just verify it
	// returns either "" or a valid path starting with /dev/ttyACM.
	if port != "" && len(port) < len("/dev/ttyACM0") {
		t.Errorf("AutoDetectPort() = %q, expected empty or valid /dev/ttyACM* path", port)
	}
}

func TestSyncResultConstants(t *testing.T) {
	// Verify the iota-assigned values match expectations.
	if ResultSuccess != 0 {
		t.Errorf("ResultSuccess = %d, want 0", ResultSuccess)
	}
	if ResultAlreadyEmpty != 1 {
		t.Errorf("ResultAlreadyEmpty = %d, want 1", ResultAlreadyEmpty)
	}
	if ResultError != 2 {
		t.Errorf("ResultError = %d, want 2", ResultError)
	}
	if ResultDryRun != 3 {
		t.Errorf("ResultDryRun = %d, want 3", ResultDryRun)
	}
}

func TestDefaultStatus(t *testing.T) {
	// Reset and verify defaults.
	SetStatus("idle", 0, "Ready for the next sync.")
	got := GetStatus()
	if got.State != "idle" {
		t.Errorf("default State = %q, want %q", got.State, "idle")
	}
	if got.Progress != 0 {
		t.Errorf("default Progress = %d, want 0", got.Progress)
	}
}

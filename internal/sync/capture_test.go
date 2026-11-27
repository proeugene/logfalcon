package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proeugene/logfalcon/internal/storage"
)

// A header-only sample is sufficient to test declarations. It deliberately
// carries no frame data and makes no claim about usable gyro samples.
const captureHeader = "H Product:Blackbox flight data recorder by Nicholas Sherlock\n" +
	"H Log start datetime:0000-01-01T00:00:00.000+00:00\n" +
	"H Field I name:loopIteration,time,gyroADC[0],gyroADC[1],gyroADC[2]"

func TestCaptureAcrossCollections(t *testing.T) {
	o, peer := transferFixture(t, 0, "")
	full := captureHeader + ",gyroUnfilt[0],gyroUnfilt[1],gyroUnfilt[2]\n"
	filtered := captureHeader + "\n"
	collect := func(data, outcome, reference string) *storage.CaptureCheck {
		t.Helper()
		peer.data = []byte(data)
		if result := o.Run("fake"); result != ResultSuccess {
			t.Fatalf("sync %v, status %+v", result, GetStatus())
		}
		saved := GetStatus().SavedCopy
		check, err := storage.ReadCaptureCheck(o.Config.StoragePath, saved.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if check.Comparison.Outcome != outcome || check.Comparison.ReferenceSessionID != reference {
			t.Fatalf("comparison %+v, want %q relative to %q", check.Comparison, outcome, reference)
		}
		return check
	}

	first := collect(full, storage.CaptureUnavailable, "")
	if len(first.Recordings) != 1 || first.Recordings[0].Gyro == nil || *first.Recordings[0].Gyro != (storage.GyroFields{Filtered: 7, Unfiltered: 7}) {
		t.Fatalf("first header evidence %+v", first.Recordings)
	}
	repeat := collect(full, storage.CaptureIdentical, first.SessionID)
	peer.fault = "request-error"
	if result := o.Run("fake"); result != ResultError {
		t.Fatalf("interrupted copy returned %v", result)
	}
	peer.fault = ""
	appended := collect(full+filtered, storage.CaptureAppended, repeat.SessionID)
	if len(appended.Recordings) != 2 || appended.Comparison.IsNewHeader(appended.Recordings[0]) ||
		!appended.Comparison.IsNewHeader(appended.Recordings[1]) || *appended.Recordings[1].Gyro != (storage.GyroFields{Filtered: 7}) {
		t.Fatalf("appended header evidence %+v", appended)
	}
	padded := collect(full+filtered+"\xff", storage.CaptureChanged, appended.SessionID)
	_ = collect(full+filtered+"\x00"+filtered, storage.CaptureChanged, padded.SessionID)
	if peer.eraseCount != 0 {
		t.Fatal("retained captures sent an erase")
	}
}

func TestEraseAttemptRetiresCaptureReference(t *testing.T) {
	o, p := transferFixture(t, 4096, "")
	if o.Run("fake") != ResultSuccess {
		t.Fatal(GetStatus())
	}
	id := GetStatus().SavedCopy.SessionID
	o.Config.EraseAfterSync = true
	p.fault = "erase-write-error"
	if o.Run("fake") != ResultError || GetStatus().ReasonCode != "erase_unconfirmed" {
		t.Fatal(GetStatus())
	}
	o.Config.EraseAfterSync = false
	p.fault = ""
	if o.Run("fake") != ResultSuccess {
		t.Fatal(GetStatus())
	}
	check, err := storage.ReadCaptureCheck(o.Config.StoragePath, GetStatus().SavedCopy.SessionID)
	if err != nil || check.Comparison.Outcome != storage.CaptureUnavailable {
		t.Fatalf("reused pre-erase reference %q: check %+v, err %v", id, check, err)
	}
}

func TestCaptureCheckWriteFailureIsAdvisory(t *testing.T) {
	for _, erase := range []bool{false, true} {
		name := "retain"
		if erase {
			name = "erase"
		}
		t.Run(name, func(t *testing.T) {
			o, peer := transferFixture(t, 4096, "")
			o.Config.EraseAfterSync = erase
			// The comparison cannot replace this directory with its sidecar.
			peer.onFirstRead = func() error {
				files, err := filepath.Glob(filepath.Join(o.Config.StoragePath, "*", "*", "*.partial"))
				if err != nil || len(files) != 1 {
					t.Fatalf("partial files %v, err %v", files, err)
				}
				return os.Mkdir(filepath.Join(filepath.Dir(files[0]), "capture-check.json"), 0755)
			}
			if o.Run("fake") != ResultSuccess {
				t.Fatal(GetStatus())
			}
			status := GetStatus()
			if status.State != "complete" || status.SavedCopy.EraseCompleted != erase || status.SavedCopy.ControllerDataRetained == erase {
				t.Fatalf("advisory write affected copy outcome: %+v", status)
			}
			path := filepath.Join(o.Config.StoragePath, filepath.FromSlash(status.SavedCopy.SessionID), storage.ManifestFilename)
			b, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(b), `"sha256"`) {
				t.Fatalf("saved manifest unavailable: %v", err)
			}
			if _, err := storage.ReadCaptureCheck(o.Config.StoragePath, status.SavedCopy.SessionID); err == nil {
				t.Fatal("accepted unavailable capture check")
			}
		})
	}
}

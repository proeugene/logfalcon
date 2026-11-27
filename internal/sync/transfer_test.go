package sync

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/led"
	"github.com/proeugene/logfalcon/internal/msp"
	"github.com/proeugene/logfalcon/internal/storage"
	goSerial "go.bug.st/serial"
)

type silentLED struct{}

func (silentLED) Set(bool)        {}
func (silentLED) DisableTrigger() {}
func (silentLED) RestoreTrigger() {}

// wirePeer responds to the actual MSP request frames issued by Run, including
// the handshake. It does not call the orchestration helpers directly.
type wirePeer struct {
	goSerial.Port
	data         []byte
	pending      []byte
	timeout      time.Duration
	fault        string
	eraseCount   int
	requests     int
	apiRequests  int
	apiSilentFor int
	variant      string
	onFirstRead  func() error
	onErase      func()
}

func (p *wirePeer) SetReadTimeout(d time.Duration) error { p.timeout = d; return nil }
func (p *wirePeer) Close() error                         { return nil }
func (p *wirePeer) Read(b []byte) (int, error) {
	if p.fault == "disconnect" && p.requests > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if len(p.pending) == 0 {
		time.Sleep(p.timeout)
		return 0, nil
	}
	// Split frames across reads as a physical serial port does.
	if len(b) > 233 {
		b = b[:233]
	}
	n := copy(b, p.pending)
	p.pending = p.pending[n:]
	return n, nil
}
func (p *wirePeer) Write(b []byte) (int, error) {
	if p.fault == "panic-handshake" {
		panic("injected handshake panic")
	}
	decoder := msp.NewFrameDecoder()
	decoder.Feed(b)
	if len(decoder.Frames) != 1 {
		return 0, fmt.Errorf("invalid request")
	}
	f := decoder.Frames[0]
	var payload []byte
	switch f.Code {
	case msp.MSPAPIVersion:
		p.apiRequests++
		if p.apiRequests <= p.apiSilentFor {
			return len(b), nil
		}
		payload = []byte{0, 1, 46}
	case msp.MSPFCVariant:
		payload = []byte("BTFL")
		if p.variant != "" {
			payload = []byte(p.variant)
		}
	case msp.MSPFCVersion:
		payload = []byte{4, 5, 2}
	case msp.MSPUID:
		payload = bytes.Repeat([]byte{7}, 12)
	case msp.MSPBlackboxConfig:
		payload = []byte{1, 1, 1, 1, 0, 0, 0}
		if p.fault == "sd-card" {
			payload[1] = msp.BlackboxDeviceSDCard
		}
	case msp.MSPDataflashSummary:
		payload = make([]byte, 13)
		payload[0] = 3
		if p.fault == "flash-busy" {
			payload[0] = 2
		}
		binary.LittleEndian.PutUint32(payload[1:5], 16)
		binary.LittleEndian.PutUint32(payload[5:9], 32*1024*1024)
		if p.eraseCount == 0 {
			binary.LittleEndian.PutUint32(payload[9:13], uint32(len(p.data)))
		}
	case msp.MSPDataflashRead:
		p.requests++
		if p.requests == 1 && p.onFirstRead != nil {
			if err := p.onFirstRead(); err != nil {
				return 0, err
			}
		}
		if p.fault == "panic-copy" {
			panic("injected copy panic")
		}
		if p.fault == "short-write" {
			return len(b) - 1, nil
		}
		if p.fault == "request-error" {
			return 0, io.ErrClosedPipe
		}
		if p.fault == "stalled" || p.fault == "disconnect" {
			return len(b), nil
		}
		address := binary.LittleEndian.Uint32(f.Payload[:4])
		size := binary.LittleEndian.Uint16(f.Payload[4:6])
		if size == 0 {
			return 0, fmt.Errorf("zero-byte read request")
		}
		end := int(address) + int(size)
		if end > len(p.data) {
			return 0, fmt.Errorf("read past end")
		}
		// FCs can return fewer bytes than requested due to their MSP buffer limit.
		if size > 512 {
			size = 512
			end = int(address) + 512
		}
		raw := p.data[int(address):end]
		if p.fault == "zero" {
			raw = nil
		}
		if p.fault == "oversized" {
			raw = make([]byte, int(size)+4096)
		}
		payload = make([]byte, 7+len(raw))
		binary.LittleEndian.PutUint32(payload[:4], address)
		if p.fault == "wrong-address" {
			binary.LittleEndian.PutUint32(payload[:4], address+1)
		}
		binary.LittleEndian.PutUint16(payload[4:6], uint16(len(raw)))
		copy(payload[7:], raw)
		if p.fault == "malformed" {
			payload = payload[:6]
		}
	case msp.MSPDataflashErase:
		if p.fault == "erase-write-error" {
			return 0, io.ErrClosedPipe
		}
		p.eraseCount++
		if p.onErase != nil {
			p.onErase()
		}
		return len(b), nil
	default:
		return 0, fmt.Errorf("unexpected code %d", f.Code)
	}
	response := msp.EncodeV2(f.Code, payload)
	response[2] = '>'
	if p.fault == "crc" && f.Code == msp.MSPDataflashRead {
		response[len(response)-1] ^= 255
	}
	p.pending = append(p.pending, response...)
	return len(b), nil
}

func TestCompletionIdentifiesDurableCopy(t *testing.T) {
	for _, erase := range []bool{false, true} {
		t.Run(fmt.Sprint(erase), func(t *testing.T) {
			o, _ := transferFixture(t, 65537, "")
			o.Config.EraseAfterSync = erase
			if o.Run("fake") != ResultSuccess {
				t.Fatal(GetStatus())
			}
			sessions, err := storage.ListSessions(o.Config.StoragePath)
			if err != nil || len(sessions) != 1 {
				t.Fatalf("sessions=%v err=%v", sessions, err)
			}
			status := GetStatus()
			saved := status.SavedCopy
			if saved == nil || saved.SessionID != sessions[0].SessionID || saved.ControllerUID != sessions[0].Manifest.FC.UID || saved.Bytes != 65537 {
				t.Fatalf("completion did not identify saved copy: %+v", status)
			}
			if saved.ControllerDataRetained == erase || saved.EraseAttempted != erase || saved.EraseCompleted != erase || status.BytesCopied != 65537 || status.TotalBytes != 65537 {
				t.Fatalf("incorrect saved facts: %+v status=%+v", saved, status)
			}
		})
	}
}

func TestKnownFailuresKeepTheirReason(t *testing.T) {
	for _, tc := range []struct{ fault, reason string }{{"sd-card", "fc_sd_card"}, {"flash-busy", "flash_busy"}, {"disconnect", "copy_failed"}, {"erase-write-error", "erase_unconfirmed"}} {
		t.Run(tc.fault, func(t *testing.T) {
			o, _ := transferFixture(t, 4096, tc.fault)
			o.Config.EraseAfterSync = true
			if o.Run("fake") != ResultError {
				t.Fatal("failure reported success")
			}
			status := GetStatus()
			if status.ReasonCode != tc.reason {
				t.Fatalf("reason=%q want %q; status=%+v", status.ReasonCode, tc.reason, status)
			}
			if tc.fault == "erase-write-error" {
				if status.SavedCopy == nil || !status.SavedCopy.EraseAttempted || status.SavedCopy.EraseCompleted || status.SavedCopy.ControllerDataRetained {
					t.Fatalf("unconfirmed erase facts=%+v", status.SavedCopy)
				}
			} else if status.SavedCopy != nil {
				t.Fatal("failed copy advertised as saved")
			}
		})
	}
}

func TestReadinessRecoversTransientStartup(t *testing.T) {
	for _, fault := range []string{"port-not-yet-present", "controller-not-yet-ready"} {
		t.Run(fault, func(t *testing.T) {
			o, p := transferFixture(t, 4096, "")
			o.Config.SerialTimeout = .02
			attempts := 0
			o.OpenPort = func(string, *goSerial.Mode) (goSerial.Port, error) {
				attempts++
				if fault == "port-not-yet-present" && attempts == 1 {
					return nil, syscall.ENOENT
				}
				return p, nil
			}
			if fault == "controller-not-yet-ready" {
				p.apiSilentFor = 2
			}
			if o.Run("fake") != ResultSuccess || p.eraseCount != 0 {
				t.Fatalf("startup did not recover: %+v", GetStatus())
			}
		})
	}
}

func TestReadinessDoesNotRetryPermanentFailures(t *testing.T) {
	t.Run("permission", func(t *testing.T) {
		o, p := transferFixture(t, 4096, "")
		attempts := 0
		o.OpenPort = func(string, *goSerial.Mode) (goSerial.Port, error) {
			attempts++
			return nil, syscall.EACCES
		}
		if o.Run("fake") != ResultError || attempts != 1 || p.requests != 0 || GetStatus().ReasonCode != "serial_unavailable" {
			t.Fatalf("permanent open failure retried or unidentified: attempts=%d status=%+v", attempts, GetStatus())
		}
	})
	t.Run("unsupported-controller", func(t *testing.T) {
		o, p := transferFixture(t, 4096, "")
		p.variant = "ARDU"
		if o.Run("fake") != ResultError || p.apiRequests > 2 || p.requests != 0 || GetStatus().ReasonCode != "unsupported_controller" {
			t.Fatalf("unsupported controller retried or copied: %+v", GetStatus())
		}
	})
}

func transferFixture(t *testing.T, size int, fault string) (*Orchestrator, *wirePeer) {
	t.Helper()
	cfg := config.Default()
	cfg.StoragePath = t.TempDir()
	cfg.StatusPath = filepath.Join(t.TempDir(), "status.json")
	cfg.SerialTimeout = .01
	if fault == "" {
		cfg.SerialTimeout = 1
	} // Coverage/race scheduling must not mimic a physical timeout.
	cfg.MinFreeSpaceMB = 0
	peer := &wirePeer{data: make([]byte, size), fault: fault}
	for i := range peer.data {
		peer.data[i] = byte(i * 17)
	}
	ConfigureStatus(cfg.StatusPath)
	t.Cleanup(func() { ConfigureStatus("") })
	return &Orchestrator{Config: cfg, LED: led.NewWithBackend(silentLED{}), OpenPort: func(string, *goSerial.Mode) (goSerial.Port, error) { return peer, nil }}, peer
}

func TestRunTransferBoundaries(t *testing.T) {
	for _, size := range []int{0, 4096, 65535, 65536, 65537, 100000, 1024 * 1024, 16 * 1024 * 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			o, p := transferFixture(t, size, "")
			result := o.Run("fake")
			if size == 0 {
				if result != ResultAlreadyEmpty {
					t.Fatal(result)
				}
				return
			}
			if result != ResultSuccess {
				t.Fatalf("result=%v status=%+v", result, GetStatus())
			}
			if p.timeout <= 0 || p.timeout > 250*time.Millisecond {
				t.Fatal("physical read timeout not configured")
			}
			sessions, err := storage.ListSessions(o.Config.StoragePath)
			if err != nil || len(sessions) != 1 {
				t.Fatalf("sessions=%v err=%v", sessions, err)
			}
			got, err := os.ReadFile(*sessions[0].BBLPath)
			if err != nil || !bytes.Equal(got, p.data) {
				t.Fatal("copy did not match source", err)
			}
			if p.eraseCount != 0 {
				t.Fatal("erased by default")
			}
			if GetStatus().State != "complete" {
				t.Fatal(GetStatus())
			}
		})
	}
}

func TestRunFailureNeverErases(t *testing.T) {
	for _, fault := range []string{"short-write", "request-error", "disconnect", "stalled", "zero", "oversized", "wrong-address", "malformed", "crc", "panic-handshake", "panic-copy", "storage"} {
		t.Run(fault, func(t *testing.T) {
			o, p := transferFixture(t, 100000, fault)
			o.Config.EraseAfterSync = true
			if fault == "storage" {
				path := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				o.Config.StoragePath = path
			}
			if result := o.Run("fake"); result != ResultError {
				t.Fatalf("failure returned %v", result)
			}
			if p.eraseCount != 0 {
				t.Fatal("failure triggered erase")
			}
			sessions, _ := storage.ListSessions(o.Config.StoragePath)
			if len(sessions) != 0 {
				t.Fatal("failed copy advertised as complete")
			}
			if GetStatus().State != "error" {
				t.Fatal(GetStatus())
			}
		})
	}
}

func TestRunEraseRequiresOptIn(t *testing.T) {
	o, p := transferFixture(t, 65537, "")
	o.Config.EraseAfterSync = true
	if o.Run("fake") != ResultSuccess || p.eraseCount != 1 {
		t.Fatal("explicit erase did not complete")
	}
	sessions, _ := storage.ListSessions(o.Config.StoragePath)
	if !sessions[0].Manifest.EraseAttempted || !sessions[0].Manifest.EraseCompleted {
		t.Fatal("erase result not recorded")
	}
}

func TestEraseSkipsFlashThatChangedDuringCopy(t *testing.T) {
	o, peer := transferFixture(t, 4097, "")
	o.Config.EraseAfterSync = true
	initial := bytes.Clone(peer.data)
	peer.onFirstRead = func() error {
		peer.data = append(peer.data, []byte("new recording")...)
		return nil
	}
	result := o.Run("fake")
	if result != ResultError || peer.eraseCount != 0 {
		t.Fatalf("flash grew past the saved bytes: result=%v, erase commands=%d", result, peer.eraseCount)
	}
	status := GetStatus()
	if status.ReasonCode != "erase_skipped" || status.SavedCopy == nil || status.SavedCopy.EraseAttempted {
		t.Fatalf("expected a saved copy and a skipped erase, got %+v", status)
	}
	dir := filepath.Join(o.Config.StoragePath, status.SavedCopy.SessionID)
	saved, err := os.ReadFile(filepath.Join(dir, storage.RawFlashFilename))
	if err != nil || !bytes.Equal(saved, initial) {
		t.Fatalf("initial recording was not preserved: %v", err)
	}
	manifest, err := storage.ReadManifest(o.Config.StoragePath, status.SavedCopy.SessionID)
	if err != nil || manifest.EraseAttempted || manifest.EraseCompleted {
		t.Fatalf("incorrect erase result in manifest: %+v, %v", manifest, err)
	}
}

func TestSavedSessionCannotBeDeletedDuringControllerErase(t *testing.T) {
	o, peer := transferFixture(t, 4097, "")
	o.Config.EraseAfterSync = true
	var sessionID string
	peer.onErase = func() {
		saved := GetStatus().SavedCopy
		if saved == nil {
			t.Fatal("missing published copy at erase")
		}
		sessionID = saved.SessionID
		if err := storage.DeleteSession(o.Config.StoragePath, sessionID); err == nil {
			t.Error("deleted the saved copy while erasing controller flash")
		}
	}
	if result := o.Run("fake"); result != ResultSuccess || peer.eraseCount != 1 {
		t.Fatalf("result %v, erase count %d", result, peer.eraseCount)
	}
	bbl, err := os.ReadFile(filepath.Join(o.Config.StoragePath, sessionID, storage.RawFlashFilename))
	if err != nil || !bytes.Equal(bbl, peer.data) {
		t.Fatal("erased controller without retaining the verified bytes")
	}
	if err := storage.DeleteSession(o.Config.StoragePath, sessionID); err != nil {
		t.Fatalf("could not delete after completion: %v", err)
	}
}

func TestTransferLock(t *testing.T) {
	dir := t.TempDir()
	first, err := acquireTransferLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if second, err := acquireTransferLock(dir); err == nil {
		_ = second.Close()
		t.Fatal("concurrent transfer accepted")
	}
}

func TestPublicationAndVerificationFailureNeverErase(t *testing.T) {
	for _, fault := range []string{"verify", "publish", "manifest"} {
		t.Run(fault, func(t *testing.T) {
			o, p := transferFixture(t, 65537, "")
			o.Config.EraseAfterSync = true
			p.onFirstRead = func() error {
				dirs, err := filepath.Glob(filepath.Join(o.Config.StoragePath, "fc_*", "*"))
				if err != nil || len(dirs) != 1 {
					return fmt.Errorf("missing prepared session")
				}
				switch fault {
				case "verify":
					return os.Rename(dirs[0], dirs[0]+".moved")
				case "publish":
					return os.Mkdir(filepath.Join(dirs[0], storage.RawFlashFilename), 0755)
				default:
					return os.Mkdir(filepath.Join(dirs[0], storage.ManifestFilename), 0755)
				}
			}
			if o.Run("fake") != ResultError || p.eraseCount != 0 {
				t.Fatal("failed publication/verification erased controller")
			}
			sessions, _ := storage.ListSessions(o.Config.StoragePath)
			if len(sessions) != 0 {
				t.Fatal("uncommitted session advertised")
			}
			if GetStatus().SavedCopy != nil {
				t.Fatal("failed verification/publication advertised a saved copy")
			}
		})
	}
}

func TestDryRunNeverErases(t *testing.T) {
	o, p := transferFixture(t, 65537, "")
	o.Config.EraseAfterSync = true
	o.DryRun = true
	if o.Run("fake") != ResultDryRun || p.eraseCount != 0 {
		t.Fatal("dry run erased")
	}
	if saved := GetStatus().SavedCopy; saved == nil || !saved.ControllerDataRetained || saved.EraseAttempted || saved.EraseCompleted {
		t.Fatalf("dry run outcome=%+v", saved)
	}
}

func TestStorageWriteFailure(t *testing.T) {
	o, p := transferFixture(t, 4096, "")
	w, err := storage.NewStreamWriter(filepath.Join(t.TempDir(), "raw_flash.bbl.partial"))
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	c := msp.NewClient(p, time.Second)
	c.FCVariant = msp.BTFLVariant
	if o.readFlash(c, w, 4096, time.Now().Add(time.Second)) == nil {
		t.Fatal("closed storage accepted")
	}
}

func TestStatusFailureAfterStartNeverErases(t *testing.T) {
	o, peer := transferFixture(t, 65537, "")
	o.Config.EraseAfterSync = true
	peer.onFirstRead = func() error {
		// Replace the status destination with a directory after BeginTransfer succeeded.
		if err := os.Remove(o.Config.StatusPath); err != nil {
			return err
		}
		return os.Mkdir(o.Config.StatusPath, 0755)
	}
	if got := o.Run("fake"); got != ResultError || peer.eraseCount != 0 || StatusError() == nil {
		t.Fatalf("result=%v erases=%d error=%v", got, peer.eraseCount, StatusError())
	}
}

package sync

import (
	"encoding/json"
	"fmt"
	"github.com/proeugene/logfalcon/internal/fc"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Status is the thread-safe sync status read by the web server.
type Status struct {
	TransferID  string    `json:"transfer_id"`
	BootID      string    `json:"boot_id"`
	PID         int       `json:"pid"`
	UpdatedUTC  time.Time `json:"updated_utc"`
	State       string    `json:"state"`
	Progress    int       `json:"progress"`
	Message     string    `json:"message"`
	BytesCopied uint32    `json:"bytes_copied"`
	TotalBytes  uint32    `json:"total_bytes"`
	SpeedBPS    float64   `json:"speed_bps"`
	ETASec      int       `json:"eta_sec"`
	// FC identity — populated once the handshake completes.
	FCVariant         string `json:"fc_variant"`          // e.g. "BTFL" or "INAV"
	FCFirmwareVersion string `json:"fc_firmware_version"` // e.g. "4.5.0"
	FCAPIVersion      string `json:"fc_api_version"`      // e.g. "1.46"
	// Warning is non-empty when firmware is newer than protocol-reviewed.
	// The sync proceeds but the user is shown an amber notice.
	Warning string `json:"warning,omitempty"`
}

var (
	statusMu         sync.RWMutex
	statusPath       string
	lastPublished    time.Time
	publicationError error
	currentStatus    = Status{
		State:   "idle",
		Message: "Ready for the next sync.",
	}
)

// GetStatus returns a snapshot of the current sync status (thread-safe).
func GetStatus() Status {
	statusMu.RLock()
	path, local := statusPath, currentStatus
	statusMu.RUnlock()
	if path == "" {
		return local
	}
	return ReadStatus(path)
}

// ConfigureStatus selects the shared runtime file; a web process must not write an idle status.
func ConfigureStatus(path string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	statusPath = path
	lastPublished = time.Time{}
	publicationError = nil
}

func bootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func BeginTransfer(port string) error {
	statusMu.Lock()
	defer statusMu.Unlock()
	if statusPath != "" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(statusPath), "shutdown.pending")); err == nil {
			return fmt.Errorf("shutdown already scheduled")
		}
	}
	publicationError = nil
	currentStatus = Status{State: "identifying", Message: "Starting controller copy.", BootID: bootID(), PID: os.Getpid(), TransferID: fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())}
	slog.Info("transfer started", "transfer_id", currentStatus.TransferID, "port", port)
	return publishLocked(true)
}

func ReadStatus(path string) Status {
	unknown := Status{State: "unavailable", Message: "Transfer status cannot be read; completion is not confirmed."}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Status{State: "waiting", Message: "No transfer recorded this boot. Connect a controller."}
	}
	if err != nil {
		return unknown
	}
	defer func() { _ = f.Close() }()
	var s Status
	if err := json.NewDecoder(io.LimitReader(f, 16384)).Decode(&s); err != nil {
		return unknown
	}
	if s.BootID != bootID() {
		return Status{State: "waiting", Message: "No transfer recorded this boot. Previous-boot results are in diagnostics."}
	}
	if !s.Active() && !s.Terminal() && s.State != "idle" && s.State != "waiting" {
		return unknown
	}
	if s.Active() {
		if s.PID <= 0 || syscall.Kill(s.PID, 0) == syscall.ESRCH || time.Since(s.UpdatedUTC) > 11*time.Minute {
			s.State, s.Message = "interrupted", "Copy process stopped or stopped updating; completion is not confirmed."
		}
	}
	return s
}

func publishLocked(force bool) error {
	if statusPath == "" || (!force && time.Since(lastPublished) < time.Second) {
		return nil
	}
	currentStatus.UpdatedUTC = time.Now().UTC()
	b, err := json.Marshal(currentStatus)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(statusPath), 0755)
	}
	if err == nil {
		err = writeStatusFile(statusPath, b)
	}
	if err != nil {
		publicationError = err
		slog.Error("status publication failed", "error", err)
		return err
	}
	lastPublished = time.Now()
	return nil
}

func writeStatusFile(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0644); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func acquireTransferLock(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "transfer.lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another transfer is running: %w", err)
	}
	return f, nil
}

// SetStatus updates the sync status (thread-safe).
// FC identity fields and Warning are preserved from the previous status so
// they remain visible throughout the sync session.
func SetStatus(state string, progress int, message string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	prev := currentStatus
	currentStatus = Status{
		TransferID: prev.TransferID, BootID: prev.BootID, PID: prev.PID,
		State:             state,
		Progress:          progress,
		Message:           message,
		FCVariant:         prev.FCVariant,
		FCFirmwareVersion: prev.FCFirmwareVersion,
		FCAPIVersion:      prev.FCAPIVersion,
		Warning:           prev.Warning,
	}
	_ = publishLocked(true) // error is latched and checked by the orchestrator
}

// setFCIdentity stores the FC identity fields after a successful handshake.
// Call this before SetStatus so subsequent SetStatus calls preserve them.
func setFCIdentity(info *fc.FCInfo) {
	statusMu.Lock()
	defer statusMu.Unlock()
	currentStatus.FCVariant = info.Variant
	currentStatus.FCFirmwareVersion = info.FirmwareVersion
	currentStatus.FCAPIVersion = fmt.Sprintf("%d.%d", info.APIMajor, info.APIMinor)
	currentStatus.Warning = info.Warning
}

// SetStatusSync updates sync status with real-time transfer metrics (thread-safe).
// Used during the flash-read loop to emit byte counter, speed, and ETA.
func SetStatusSync(state string, progress int, message string, bytesCopied, totalBytes uint32, speedBPS float64, etaSec int) {
	statusMu.Lock()
	defer statusMu.Unlock()
	prev := currentStatus
	currentStatus = Status{
		TransferID: prev.TransferID, BootID: prev.BootID, PID: prev.PID,
		State:             state,
		Progress:          progress,
		Message:           message,
		BytesCopied:       bytesCopied,
		TotalBytes:        totalBytes,
		SpeedBPS:          speedBPS,
		ETASec:            etaSec,
		FCVariant:         prev.FCVariant,
		FCFirmwareVersion: prev.FCFirmwareVersion,
		FCAPIVersion:      prev.FCAPIVersion,
		Warning:           prev.Warning,
	}
	_ = publishLocked(false) // error is latched and checked by the orchestrator
}

// Active enumerates the only states allowed to prevent idle shutdown.
func (s Status) Active() bool {
	switch s.State {
	case "identifying", "querying", "syncing", "verifying", "erasing":
		return true
	}
	return false
}
func (s Status) Terminal() bool {
	switch s.State {
	case "complete", "empty", "error", "interrupted":
		return true
	}
	return false
}

// StatusError is sticky for a transfer: recovering a later write must not hide a gap.
func StatusError() error { statusMu.RLock(); defer statusMu.RUnlock(); return publicationError }

// AcquireTransferLock also allows idle shutdown to exclude a new USB transfer.
func AcquireTransferLock(dir string) (*os.File, error) { return acquireTransferLock(dir) }

// ScheduleShutdown reserves the appliance under the same lock used by USB copies.
func ScheduleShutdown(dir string, shutdown func() error) error {
	lock, err := acquireTransferLock(dir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	path := filepath.Join(dir, "shutdown.pending")
	if err = os.WriteFile(path, []byte("scheduled\n"), 0644); err != nil {
		return err
	}
	if err = shutdown(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

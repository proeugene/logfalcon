// Package sync implements the 10-step blackbox flash sync state machine.
//
// Steps:
//  1. Open serial port → create MSP client
//  2. Identify FC (MSP handshake, verify supported variant)
//  3. Query flash state (dataflash summary)
//  4. Check Pi storage (free space, cleanup if needed)
//  5. Prepare output (session dir + stream writer)
//  6. Stream flash read → file (pipelined, with retry)
//  7. Verify integrity (size + SHA-256)
//  8. Write manifest
//  9. Erase FC flash (poll until empty or timeout)
//  10. Signal result (LED + status)
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	goSerial "go.bug.st/serial"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/fc"
	"github.com/proeugene/logfalcon/internal/led"
	"github.com/proeugene/logfalcon/internal/msp"
	"github.com/proeugene/logfalcon/internal/storage"
	"github.com/proeugene/logfalcon/internal/util"
)

const (
	maxConsecutiveErrors = 5
	erasePollInterval    = 2 * time.Second
	maxSyncDuration      = 10 * time.Minute
	maxFlashUsedSize     = 128 * 1024 * 1024 // 128 MB OOM safety cap
)

// SyncResult represents the outcome of a sync operation.
type SyncResult int

const (
	ResultSuccess SyncResult = iota
	ResultAlreadyEmpty
	ResultError
	ResultDryRun
)

// Orchestrator runs the full blackbox sync workflow.
type Orchestrator struct {
	Config *config.Config
	LED    *led.Controller
	DryRun bool
	// OpenPort is injectable for wire-level regression tests. Production uses serial.Open.
	OpenPort        func(string, *goSerial.Mode) (goSerial.Port, error)
	readUpdateState func(string) ([]byte, error) // defaults to os.ReadFile
}

// Run opens the serial port and executes the 10-step sync workflow.
func (o *Orchestrator) Run(portPath string) (result SyncResult) {
	result = ResultError
	if err := o.Config.Validate(); err != nil {
		slog.Error("invalid sync configuration", "error", err)
		return
	}
	lock, err := acquireTransferLock(filepath.Dir(o.Config.StatusPath))
	if err != nil {
		slog.Error("cannot acquire transfer lock", "error", err)
		return
	}
	defer func() { _ = lock.Close() }()
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	// Read recovery state while holding the same lock as the updater.
	readState := o.readUpdateState
	if readState == nil {
		readState = os.ReadFile
	}
	if b, err := readState("/var/lib/logfalcon-update/state/status.json"); err == nil {
		var u struct{ Phase string }
		if json.Unmarshal(b, &u) != nil {
			slog.Error("update recovery pending; no transfer started")
			return ResultError
		}
		switch u.Phase {
		case "idle", "validating", "success", "rolled-back", "failed":
		default:
			slog.Error("update activation or unreadable recovery state; no transfer started")
			return ResultError
		}
	} else if !os.IsNotExist(err) {
		slog.Error("update state unreadable")
		return ResultError
	}
	if err := BeginTransfer(portPath); err != nil {
		slog.Error("cannot start without shared status", "error", err)
		return
	}
	logger := slog.Default()
	slog.SetDefault(logger.With("transfer_id", currentStatus.TransferID))
	defer slog.SetDefault(logger)
	defer func() {
		if r := recover(); r != nil {
			result = ResultError
			slog.Error("panic during sync", "panic_type", fmt.Sprintf("%T", r))
			o.LED.SetState(led.Error)
			SetStatus("error", 0, "Unexpected sync error. Check the service log for details.")
		}
	}()

	result, err = o.run(portPath)
	if err == nil {
		err = StatusError()
	}
	if err != nil {
		slog.Error("sync error", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Unexpected sync error. Check the service log for details.")
		return ResultError
	}
	return result
}

func (o *Orchestrator) run(portPath string) (SyncResult, error) {
	cfg := o.Config
	totalStarted := time.Now()
	syncDeadline := totalStarted.Add(maxSyncDuration)
	timings := make(map[string]float64)

	// --- Step 1: Open serial port ---
	slog.Info("step 1: opening serial port", "port", portPath, "baud", cfg.SerialBaud)
	openPort := o.OpenPort
	if openPort == nil {
		openPort = goSerial.Open
	}
	port, err := openPort(portPath, &goSerial.Mode{
		BaudRate: cfg.SerialBaud,
		DataBits: 8,
		StopBits: goSerial.OneStopBit,
		Parity:   goSerial.NoParity,
	})
	if err != nil {
		o.LED.SetState(led.Error)
		SetStatus("error", 0, fmt.Sprintf("Could not open serial port %s.", portPath))
		return ResultError, nil
	}
	defer func() { _ = port.Close() }()
	watchdog := time.AfterFunc(maxSyncDuration, func() { _ = port.Close() })
	defer watchdog.Stop()
	// Bound the underlying blocking read so the MSP and overall deadlines can run.
	readPoll := time.Duration(cfg.SerialTimeout * float64(time.Second))
	if readPoll > 250*time.Millisecond {
		readPoll = 250 * time.Millisecond
	}
	if err := port.SetReadTimeout(readPoll); err != nil {
		SetStatus("error", 0, "Could not configure the serial read timeout.")
		return ResultError, err
	}

	timeout := time.Duration(cfg.SerialTimeout * float64(time.Second))
	client := msp.NewClient(port, timeout)
	client.SetDeadline(syncDeadline)

	// --- Step 2: Identify FC ---
	// Clear any FC identity from the previous session so the dashboard doesn't
	// show stale version info while the new handshake is in progress.
	slog.Info("step 2: identifying FC", "port", portPath)
	statusMu.Lock()
	currentStatus.FCVariant = ""
	currentStatus.FCFirmwareVersion = ""
	currentStatus.FCAPIVersion = ""
	currentStatus.Warning = ""
	statusMu.Unlock()
	SetStatus("identifying", 0, "Checking the flight controller over MSP.")
	identifyStarted := time.Now()

	fcInfo, result := o.identifyFC(client)
	if result != nil {
		return *result, nil
	}
	setFCIdentity(fcInfo) // publish FC identity + warning to dashboard
	timings["identify_sec"] = secondsSince(identifyStarted)

	// --- Step 3: Query flash state ---
	slog.Info("step 3: querying flash state")
	SetStatus("querying", 0, "Reading blackbox flash usage from the FC.")
	queryStarted := time.Now()

	usedSize, result := o.queryFlashState(client)
	if result != nil {
		return *result, nil
	}
	timings["query_sec"] = secondsSince(queryStarted)

	// --- Step 4: Check Pi storage ---
	slog.Info("step 4: checking Pi storage")
	sessionDir, writer, result := o.checkStorageAndPrepare(fcInfo, usedSize)
	if result != nil {
		return *result, nil
	}
	defer func() { _ = writer.KeepPartial(); writer.Release() }()

	// --- Step 6: Stream flash read ---
	slog.Info("step 6: reading flash", "bytes", usedSize, "dir", sessionDir)
	o.LED.SetState(led.Busy)
	SetStatus("syncing", 0, "Copying blackbox flash to the Pi SD card.")
	streamStarted := time.Now()

	result = o.readFlash(client, writer, usedSize, syncDeadline)
	if result != nil {
		return *result, nil
	}
	timings["stream_sec"] = secondsSince(streamStarted)

	// --- Step 7: Verify integrity ---
	slog.Info("step 7: verifying integrity")
	o.LED.SetState(led.Busy)
	SetStatus("verifying", 0, "Verifying the copied file before erase.")
	verifyStarted := time.Now()

	fileSHA256, result := o.verifyIntegrity(writer, usedSize)
	if result != nil {
		return *result, nil
	}
	timings["verify_sec"] = secondsSince(verifyStarted)

	// Publish only an exact-length, flushed and reread-verified copy.
	if err := writer.Commit(filepath.Join(sessionDir, storage.RawFlashFilename)); err != nil {
		SetStatus("error", 0, "Could not publish the saved log.")
		return ResultError, err
	}

	// --- Step 8: Write manifest ---
	slog.Info("step 8: writing manifest")
	timings["total_sec"] = secondsSince(totalStarted)
	storageInfo := fcInfoToStorage(fcInfo)
	if err := storage.WriteManifest(cfg.StoragePath, sessionDir, storageInfo, fileSHA256, int64(usedSize),
		false, false, timings); err != nil {
		slog.Warn("failed to write manifest", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Failed to write the session manifest.")
		return ResultError, nil
	}

	if o.DryRun || !cfg.EraseAfterSync {
		if err := storage.RecordRetained(cfg.StoragePath, sessionDir); err != nil {
			SetStatus("error", 0, "Saved copy exists, but retention outcome could not be recorded.")
			return ResultError, err
		}
	}
	if o.DryRun {
		slog.Info("DRY RUN — skipping erase")
		o.LED.SetState(led.Done)
		if err := StatusError(); err != nil {
			return ResultError, err
		}
		SetStatus("complete", 100, "Copy complete. Dry run kept the FC flash untouched.")
		return ResultDryRun, nil
	}

	if !cfg.EraseAfterSync {
		slog.Info("erase_after_sync=false — skipping erase")
		o.LED.SetState(led.Done)
		if err := StatusError(); err != nil {
			return ResultError, err
		}
		SetStatus("complete", 100, "Copy complete. Erase was skipped by configuration.")
		return ResultSuccess, nil
	}

	// --- Step 9: Erase FC flash ---
	slog.Info("step 9: erasing FC flash")
	o.LED.SetState(led.Busy)
	SetStatus("erasing", 0, "Erasing the FC flash now that the copy is verified.")
	if err := StatusError(); err != nil {
		return ResultError, err
	}
	eraseStarted := time.Now()

	// Persist the erase attempt before sending a destructive command.
	if err := storage.UpdateManifestErase(cfg.StoragePath, sessionDir, false, timings); err != nil {
		SetStatus("error", 0, "Could not record the erase decision; FC flash kept untouched.")
		return ResultError, err
	}
	eraseOK := o.waitForErase(client)
	timings["erase_sec"] = secondsSince(eraseStarted)
	timings["total_sec"] = secondsSince(totalStarted)
	if err := storage.UpdateManifestErase(cfg.StoragePath, sessionDir, eraseOK, timings); err != nil {
		SetStatus("error", 0, "Copy saved, but the erase result could not be recorded.")
		return ResultError, err
	}

	if !eraseOK {
		slog.Warn("flash erase did not complete within timeout")
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Copy succeeded, but erase did not finish before timeout.")
		return ResultError, nil
	}
	slog.Info("flash erase confirmed")

	// --- Step 10: Signal result ---
	slog.Info("step 10: sync complete")
	o.LED.SetState(led.Done)
	if err := StatusError(); err != nil {
		return ResultError, err
	}
	SetStatus("complete", 100, "Sync complete — safe to unplug and fly again.")
	return ResultSuccess, nil
}

// identifyFC performs the MSP handshake and returns FC info.
// Returns (fcInfo, nil) on success or (nil, result) on failure.
func (o *Orchestrator) identifyFC(client *msp.Client) (*fc.FCInfo, *SyncResult) {
	fcInfo, err := fc.Detect(client)
	if err != nil {
		slog.Error("FC detection failed", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, err.Error())
		r := ResultError
		return nil, &r
	}

	// Tell the MSP client which variant we're talking to so it
	// can adjust response parsing (e.g. iNav vs Betaflight format).
	variant := fcInfo.Variant
	if len(variant) > 4 {
		variant = variant[:4]
	}
	client.FCVariant = variant

	slog.Info("FC identified", "variant", fcInfo.Variant, "uid", fcInfo.UID)
	return fcInfo, nil
}

// queryFlashState reads the dataflash summary and validates state.
// Returns (usedSize, nil) on success or (0, result) on failure.
func (o *Orchestrator) queryFlashState(client *msp.Client) (uint32, *SyncResult) {
	summary, err := client.GetDataflashSummary()
	if err != nil {
		slog.Error("failed to get flash summary", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not read the FC flash summary.")
		r := ResultError
		return 0, &r
	}

	slog.Info("flash summary",
		"supported", summary.Supported, "ready", summary.Ready, "used", summary.UsedSize, "total", summary.TotalSize)

	if !summary.Supported {
		slog.Warn("FC flash not supported")
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "This FC does not expose supported flash storage.")
		r := ResultError
		return 0, &r
	}

	if !summary.Ready {
		slog.Warn("FC flash not ready")
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "The FC flash is busy right now. Try again in a moment.")
		r := ResultError
		return 0, &r
	}

	if summary.UsedSize == 0 {
		slog.Info("flash is empty — nothing to sync")
		o.LED.SetState(led.Done)
		SetStatus("empty", 0, "Flash already empty — nothing to sync.")
		r := ResultAlreadyEmpty
		return 0, &r
	}

	if summary.UsedSize > summary.TotalSize || summary.TotalSize == 0 || summary.UsedSize > maxFlashUsedSize {
		slog.Warn("FC flash size exceeds safety limit", "usedSize", summary.UsedSize, "limit", maxFlashUsedSize)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "FC flash exceeds the maximum safe size (128 MB) for this device.")
		r := ResultError
		return 0, &r
	}

	return summary.UsedSize, nil
}

// checkStorageAndPrepare validates free space, cleans up if needed, and creates
// the session directory and stream writer (Steps 4–5).
func (o *Orchestrator) checkStorageAndPrepare(fcInfo *fc.FCInfo, usedSize uint32) (string, *storage.StreamWriter, *SyncResult) {
	cfg := o.Config
	storagePath := cfg.StoragePath
	if err := os.MkdirAll(storagePath, 0o755); err != nil {
		slog.Error("failed to create storage dir", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not create the storage directory.")
		r := ResultError
		return "", nil, &r
	}

	requiredMB := float64(usedSize)/(1024*1024) + float64(cfg.MinFreeSpaceMB)
	availableMB, err := util.FreeMB(storagePath)
	if err != nil {
		slog.Error("failed to check free space", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not check available storage space.")
		r := ResultError
		return "", nil, &r
	}
	slog.Info("storage check", "requiredMB", requiredMB, "availableMB", availableMB)

	if availableMB < requiredMB {
		if cfg.StoragePressureCleanup {
			SetStatus("querying", 0, "Storage is tight, cleaning up the oldest sessions first.")
			requiredBytes := int64(requiredMB * 1024 * 1024)
			deleted, cleanErr := storage.CleanupOldestSessions(storagePath, requiredBytes)
			if cleanErr != nil {
				slog.Warn("storage cleanup error", "error", cleanErr)
			}
			if len(deleted) > 0 {
				slog.Info("reclaimed storage", "deleted_sessions", len(deleted))
			}
			availableMB, _ = util.FreeMB(storagePath)
			slog.Info("storage after cleanup", "availableMB", availableMB)
		}
		if availableMB < requiredMB {
			slog.Error("insufficient Pi storage",
				"availableMB", availableMB, "requiredMB", requiredMB)
			o.LED.SetState(led.Error)
			SetStatus("error", 0, "Not enough free space on the Pi SD card to copy this log safely.")
			r := ResultError
			return "", nil, &r
		}
	}

	// --- Step 5: Prepare output ---
	slog.Info("step 5: preparing output directory")
	storageInfo := fcInfoToStorage(fcInfo)
	sessionDir, err := storage.MakeSessionDir(storagePath, storageInfo)
	if err != nil {
		slog.Error("failed to create session dir", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not create the output directory.")
		r := ResultError
		return "", nil, &r
	}

	writer, err := storage.NewSessionWriter(storagePath, sessionDir)
	if err != nil {
		slog.Error("failed to create stream writer", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not open the output file for writing.")
		r := ResultError
		return "", nil, &r
	}

	return sessionDir, writer, nil
}

// readFlash keeps one request outstanding, accepting short valid responses but
// never zero, oversized or out-of-order data as successful completion.
func (o *Orchestrator) readFlash(client *msp.Client, writer *storage.StreamWriter, usedSize uint32, deadline time.Time) (result *SyncResult) {
	fail := func(message string) *SyncResult {
		o.LED.SetState(led.Error)
		SetStatus("error", 0, message)
		r := ResultError
		return &r
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during flash read", "panic_type", fmt.Sprintf("%T", r))
			result = fail("Unexpected error while copying flash data.")
		}
		if result != nil {
			_ = writer.KeepPartial()
		}
	}()
	var address uint32
	start := time.Now()
	client.SetDeadline(deadline)
	for address < usedSize {
		if StatusError() != nil {
			return fail("Shared status failed; FC flash kept untouched.")
		}
		if time.Now().After(deadline) {
			return fail("Sync deadline exceeded; FC flash kept untouched.")
		}
		size := uint32(o.Config.FlashChunkSize)
		if remaining := usedSize - address; remaining < size {
			size = remaining
		}
		var data []byte
		accepted := false
		for attempt := 1; attempt <= maxConsecutiveErrors; attempt++ {
			if time.Now().After(deadline) {
				return fail("Sync deadline exceeded; FC flash kept untouched.")
			}
			client.FlushFrames(msp.MSPDataflashRead)
			if err := client.SendFlashReadRequest(address, uint16(size), o.Config.FlashReadCompression); err != nil {
				slog.Error("flash request write failed", "offset", address, "error", err)
				return fail("Could not send flash request; FC flash kept untouched.")
			}
			replyAddress, reply, err := client.ReceiveFlashReadResponse()
			if err == nil && replyAddress == address && len(reply) > 0 && uint32(len(reply)) <= size {
				data, accepted = reply, true
				break
			}
			slog.Warn("flash response rejected", "offset", address, "attempt", attempt, "reply_offset", replyAddress, "reply_bytes", len(reply), "error", err)
		}
		if !accepted {
			return fail("FC read failed; incomplete copy retained as .partial and FC flash kept untouched.")
		}
		if n, err := writer.Write(data); err != nil || n != len(data) {
			if err == nil {
				err = io.ErrShortWrite
			}
			slog.Error("saving flash data failed", "error", err)
			return fail("Storage write failed; FC flash kept untouched.")
		}
		address += uint32(len(data))
		progress := int(uint64(address) * 100 / uint64(usedSize))
		elapsed := time.Since(start).Seconds()
		speed := float64(address) / elapsed
		eta := int(float64(usedSize-address) / speed)
		SetStatusSync("syncing", progress, "Copying blackbox flash to the Pi SD card.", address, usedSize, speed, eta)
		if address%(uint32(o.Config.FlashChunkSize)*64) < uint32(o.Config.FlashChunkSize) {
			slog.Debug("flash read progress", "copied", address, "total", usedSize, "percent", progress)
		}
	}
	if err := writer.Close(); err != nil {
		slog.Error("finalizing saved data failed", "error", err)
		return fail("Could not flush saved data; FC flash kept untouched.")
	}
	slog.Info("flash read complete", "bytes_written", writer.BytesWritten())
	return nil
}

// verifyIntegrity checks file size and SHA-256 (Step 7).
func (o *Orchestrator) verifyIntegrity(writer *storage.StreamWriter, usedSize uint32) (string, *SyncResult) {
	if writer.BytesWritten() != int64(usedSize) {
		slog.Warn("size mismatch", "written", writer.BytesWritten(), "expected", usedSize)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "The copied file size did not match the FC flash size.")
		r := ResultError
		return "", &r
	}

	match, fileSHA256, err := writer.VerifyAgainstFile()
	if err != nil {
		slog.Error("SHA-256 verification error", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not verify the copied file integrity.")
		r := ResultError
		return "", &r
	}
	if !match {
		slog.Error("SHA-256 verification failed — NOT erasing FC flash")
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Verification failed, so the FC flash was left untouched.")
		r := ResultError
		return "", &r
	}

	slog.Info("integrity OK", "sha256", fileSHA256)
	return fileSHA256, nil
}

// waitForErase sends the erase command and polls until flash is empty or timeout.
func (o *Orchestrator) waitForErase(client *msp.Client) bool {
	if err := client.EraseFlash(); err != nil {
		slog.Error("failed to send erase command", "error", err)
		return false
	}

	deadline := time.Now().Add(time.Duration(o.Config.EraseTimeoutSec) * time.Second)
	client.SetDeadline(deadline)
	for time.Now().Before(deadline) {
		summary, err := client.GetDataflashSummary()
		if err != nil {
			slog.Warn("error polling flash summary during erase", "error", err)
		} else if summary.Supported && summary.UsedSize == 0 && summary.Ready && time.Now().Before(deadline) {
			return true
		}
		pause := time.Until(deadline)
		if pause <= 0 {
			break
		}
		if pause > erasePollInterval {
			pause = erasePollInterval
		}
		time.Sleep(pause)
	}
	return false
}

// AutoDetectPort returns the first /dev/ttyACM* port found, or "" if none.
func AutoDetectPort() string {
	matches, err := filepath.Glob("/dev/ttyACM*")
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	slog.Info("auto-detected port", "port", matches[0])
	return matches[0]
}

// fcInfoToStorage converts fc.FCInfo to storage.FCInfo to avoid circular imports.
func fcInfoToStorage(info *fc.FCInfo) *storage.FCInfo {
	return &storage.FCInfo{
		APIMajor:       info.APIMajor,
		APIMinor:       info.APIMinor,
		Variant:        info.Variant,
		UID:            info.UID,
		BlackboxDevice: info.BlackboxDevice,
	}
}

// secondsSince returns elapsed seconds since t, rounded to milliseconds.
func secondsSince(t time.Time) float64 {
	d := time.Since(t).Seconds()
	return float64(int(d*1000)) / 1000
}

// Ensure msp.Client satisfies fc.MSPClient at compile time.
var _ fc.MSPClient = (*msp.Client)(nil)

// Ensure common error types are usable with errors.As.
var (
	_ error = (*fc.DetectionError)(nil)
	_ error = (*fc.NotSupportedError)(nil)
	_ error = (*fc.SDCardError)(nil)
	_ error = (*msp.Error)(nil)
	_ error = (*msp.TimeoutError)(nil)
)

// Silence the errors import if only used for compile-time checks above.
var _ = errors.As

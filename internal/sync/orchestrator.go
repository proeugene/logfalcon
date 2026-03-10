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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
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
)

// SyncResult represents the outcome of a sync operation.
type SyncResult int

const (
	ResultSuccess      SyncResult = iota
	ResultAlreadyEmpty
	ResultError
	ResultDryRun
)

// Status is the thread-safe sync status read by the web server.
type Status struct {
	State    string `json:"state"`
	Progress int    `json:"progress"`
	Message  string `json:"message"`
}

var (
	statusMu      sync.RWMutex
	currentStatus = Status{
		State:   "idle",
		Message: "Ready for the next sync.",
	}
)

// GetStatus returns a snapshot of the current sync status (thread-safe).
func GetStatus() Status {
	statusMu.RLock()
	defer statusMu.RUnlock()
	return currentStatus
}

// SetStatus updates the sync status (thread-safe).
func SetStatus(state string, progress int, message string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	currentStatus = Status{
		State:    state,
		Progress: progress,
		Message:  message,
	}
}

// Orchestrator runs the full blackbox sync workflow.
type Orchestrator struct {
	Config *config.Config
	LED    *led.Controller
	DryRun bool
}

// Run opens the serial port and executes the 10-step sync workflow.
func (o *Orchestrator) Run(portPath string) SyncResult {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during sync", "error", r)
			o.LED.SetState(led.Error)
			SetStatus("error", 0, "Unexpected sync error. Check the service log for details.")
		}
	}()

	result, err := o.run(portPath)
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
	timings := make(map[string]float64)

	// --- Step 1: Open serial port ---
	slog.Info("step 1: opening serial port", "port", portPath, "baud", cfg.SerialBaud)
	port, err := goSerial.Open(portPath, &goSerial.Mode{
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
	defer port.Close()

	timeout := time.Duration(cfg.SerialTimeout * float64(time.Second))
	client := msp.NewClient(port, timeout)
	defer client.Close()

	// --- Step 2: Identify FC ---
	slog.Info("step 2: identifying FC", "port", portPath)
	SetStatus("identifying", 0, "Checking the flight controller over MSP.")
	identifyStarted := time.Now()

	fcInfo, result := o.identifyFC(client)
	if result != nil {
		return *result, nil
	}
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

	// --- Step 6: Stream flash read ---
	slog.Info("step 6: reading flash", "bytes", usedSize, "dir", sessionDir)
	o.LED.SetState(led.Busy)
	SetStatus("syncing", 0, "Copying blackbox flash to the Pi SD card.")
	streamStarted := time.Now()

	result = o.readFlash(client, writer, usedSize)
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

	// --- Step 8: Write manifest ---
	slog.Info("step 8: writing manifest")
	timings["total_sec"] = secondsSince(totalStarted)
	storageInfo := fcInfoToStorage(fcInfo)
	if err := storage.WriteManifest(sessionDir, storageInfo, fileSHA256, int64(usedSize),
		false, false, timings); err != nil {
		slog.Warn("failed to write manifest", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Failed to write the session manifest.")
		return ResultError, nil
	}

	if o.DryRun {
		slog.Info("DRY RUN — skipping erase")
		o.LED.SetState(led.Done)
		SetStatus("idle", 0, "Copy complete. Dry run kept the FC flash untouched.")
		return ResultDryRun, nil
	}

	if !cfg.EraseAfterSync {
		slog.Info("erase_after_sync=false — skipping erase")
		o.LED.SetState(led.Done)
		SetStatus("idle", 0, "Copy complete. Erase was skipped by configuration.")
		return ResultSuccess, nil
	}

	// --- Step 9: Erase FC flash ---
	slog.Info("step 9: erasing FC flash")
	o.LED.SetState(led.Busy)
	SetStatus("erasing", 0, "Erasing the FC flash now that the copy is verified.")
	eraseStarted := time.Now()

	eraseOK := o.waitForErase(client)
	timings["erase_sec"] = secondsSince(eraseStarted)
	timings["total_sec"] = secondsSince(totalStarted)
	_ = storage.UpdateManifestErase(sessionDir, eraseOK, timings)

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
	SetStatus("idle", 0, "Sync complete — safe to unplug and fly again.")
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
		SetStatus("idle", 0, "Flash already empty — nothing to sync.")
		r := ResultAlreadyEmpty
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

	bblPath := filepath.Join(sessionDir, storage.RawFlashFilename)
	writer, err := storage.NewStreamWriter(bblPath)
	if err != nil {
		slog.Error("failed to create stream writer", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not open the output file for writing.")
		r := ResultError
		return "", nil, &r
	}

	return sessionDir, writer, nil
}

// readFlash streams flash data from the FC using pipelined reads (Step 6).
func (o *Orchestrator) readFlash(client *msp.Client, writer *storage.StreamWriter, usedSize uint32) *SyncResult {
	cfg := o.Config
	var address uint32
	consecutiveErrors := 0
	chunkSize := uint16(cfg.FlashChunkSize)
	compression := cfg.FlashReadCompression

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic during flash read", "error", r)
			_ = writer.Abort()
			o.LED.SetState(led.Error)
			SetStatus("error", 0, "Unexpected error while copying flash data.")
		}
	}()

	// Send first request (prime the pipeline).
	firstChunkSize := chunkSize
	if remaining := usedSize - address; uint16(remaining) < firstChunkSize {
		firstChunkSize = uint16(remaining)
	}
	if err := client.SendFlashReadRequest(address, firstChunkSize, compression); err != nil {
		slog.Error("failed to send initial flash read request", "error", err)
		_ = writer.Abort()
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Could not start reading flash data from the FC.")
		r := ResultError
		return &r
	}

	for address < usedSize {
		chunkAddr, data, err := client.ReceiveFlashReadResponse()
		if err != nil {
			consecutiveErrors++
			slog.Warn("flash read error", "address", fmt.Sprintf("0x%08x", address),
				"attempt", consecutiveErrors, "maxAttempts", maxConsecutiveErrors, "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				slog.Error("too many consecutive read errors — aborting")
				_ = writer.Abort()
				o.LED.SetState(led.Error)
				SetStatus("error", 0, "Too many FC read errors. Try another USB cable and sync again.")
				r := ResultError
				return &r
			}
			time.Sleep(100 * time.Millisecond)
			// Re-send the same request on error.
			retrySize := chunkSize
			if remaining := usedSize - address; uint16(remaining) < retrySize {
				retrySize = uint16(remaining)
			}
			_ = client.SendFlashReadRequest(address, retrySize, compression)
			continue
		}

		if chunkAddr != address {
			slog.Warn("address mismatch — retrying", "expected", fmt.Sprintf("0x%08x", address), "got", fmt.Sprintf("0x%08x", chunkAddr))
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				slog.Error("too many address mismatches — aborting")
				_ = writer.Abort()
				o.LED.SetState(led.Error)
				SetStatus("error", 0, "The FC returned inconsistent data. Reconnect and try again.")
				r := ResultError
				return &r
			}
			retrySize := chunkSize
			if remaining := usedSize - address; uint16(remaining) < retrySize {
				retrySize = uint16(remaining)
			}
			_ = client.SendFlashReadRequest(address, retrySize, compression)
			continue
		}

		if len(data) == 0 {
			slog.Info("FC returned 0 bytes — end of data", "address", fmt.Sprintf("0x%08x", address))
			break
		}

		consecutiveErrors = 0

		// Pipeline: send next request BEFORE processing current data.
		nextAddr := address + uint32(len(data))
		if nextAddr < usedSize {
			nextChunkSize := chunkSize
			if remaining := usedSize - nextAddr; uint16(remaining) < nextChunkSize {
				nextChunkSize = uint16(remaining)
			}
			_ = client.SendFlashReadRequest(nextAddr, nextChunkSize, compression)
		}

		if _, err := writer.Write(data); err != nil {
			slog.Error("failed to write flash data", "error", err)
			_ = writer.Abort()
			o.LED.SetState(led.Error)
			SetStatus("error", 0, "Failed to write data to the output file.")
			r := ResultError
			return &r
		}
		address = nextAddr

		progress := int(address * 100 / usedSize)
		SetStatus("syncing", progress, "Copying blackbox flash to the Pi SD card.")
		if address%(uint32(chunkSize)*64) < uint32(chunkSize) {
			slog.Debug("flash read progress", "address", fmt.Sprintf("0x%08x", address),
				"total", fmt.Sprintf("0x%08x", usedSize), "percent", progress)
		}
	}

	if err := writer.Close(); err != nil {
		slog.Error("failed to close writer", "error", err)
		o.LED.SetState(led.Error)
		SetStatus("error", 0, "Failed to finalize the output file.")
		r := ResultError
		return &r
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
	for time.Now().Before(deadline) {
		time.Sleep(erasePollInterval)
		summary, err := client.GetDataflashSummary()
		if err != nil {
			slog.Warn("error polling flash summary during erase", "error", err)
			continue
		}
		slog.Debug("erase poll", "used", summary.UsedSize, "ready", summary.Ready)
		if summary.UsedSize == 0 && summary.Ready {
			return true
		}
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

// Package diagnostics collects bounded, redacted appliance evidence using the system journal.
package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/util"
)

func BootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}
func Uptime() float64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var seconds float64
	_, _ = fmt.Sscan(string(b), &seconds)
	return seconds
}

type contextualHandler struct {
	slog.Handler
	started time.Time
	uptime  float64
}

func (h contextualHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.Float64("uptime_sec", h.uptime+time.Since(h.started).Seconds()))
	return h.Handler.Handle(ctx, r)
}
func (h contextualHandler) WithAttrs(a []slog.Attr) slog.Handler {
	h.Handler = h.Handler.WithAttrs(a)
	return h
}
func (h contextualHandler) WithGroup(g string) slog.Handler {
	h.Handler = h.Handler.WithGroup(g)
	return h
}

func ConfigureLogging(debug bool, version, commit string) {
	slog.SetDefault(newLogger(os.Stderr, debug, version, commit))
}

func newLogger(w io.Writer, debug bool, version, commit string) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(contextualHandler{handler, time.Now(), Uptime()}).With("version", version, "commit", commit, "boot_id", BootID(), "pid", os.Getpid())
}

type BootState struct {
	BootID                string    `json:"boot_id"`
	StartedUTC            time.Time `json:"started_utc"`
	CleanStop             bool      `json:"clean_service_stop"`
	PreviousRunIncomplete bool      `json:"previous_run_incomplete"`
	PreviousBootID        string    `json:"previous_boot_id,omitempty"`
}

// Lifecycle records service start/stop, not proof of a completed OS shutdown.
func Lifecycle(dir, action, boot string) error {
	path := filepath.Join(dir, "boot-state.json")
	var state BootState
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &state); err != nil {
			return fmt.Errorf("invalid lifecycle marker")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	switch action {
	case "start":
		if state.BootID == boot && !state.CleanStop {
			return nil
		}
		previous := state
		state = BootState{BootID: boot, StartedUTC: time.Now().UTC(), PreviousRunIncomplete: previous.BootID != "" && !previous.CleanStop, PreviousBootID: previous.BootID}
		slog.Info("appliance boot lifecycle started", "previous_run_incomplete", state.PreviousRunIncomplete, "previous_boot_id", state.PreviousBootID, "cause", "unknown if previous run was incomplete")
	case "stop":
		if state.BootID != boot {
			return fmt.Errorf("lifecycle marker belongs to another boot")
		}
		state.CleanStop = true
		slog.Info("appliance shutdown lifecycle reached")
	default:
		return fmt.Errorf("invalid lifecycle action")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return atomicWrite(path, data, true)
}

type Options struct {
	BootDir, StateDir, Version, Commit string
	Config                             *config.Config
	// Journal injection permits export tests without systemd or hardware.
	Journal func(context.Context, ...string) ([]byte, error)
}

type Snapshot struct {
	GeneratedUTC time.Time         `json:"generated_utc"`
	BootID       string            `json:"boot_id"`
	UptimeSec    float64           `json:"uptime_sec"`
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	Config       map[string]any    `json:"redacted_config"`
	Lifecycle    *BootState        `json:"lifecycle,omitempty"`
	Boots        string            `json:"journal_boots"`
	Logs         map[string]string `json:"logs"`
	Warnings     []string          `json:"warnings"`
}

var secretLine = regexp.MustCompile(`(?i)(password|passphrase|wpa_psk|psk|csrf_token|token)(["\s]*[:=]\s*)[^\r\n]*`)

func Redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= 4 {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return secretLine.ReplaceAllString(s, "${1}${2}[REDACTED]")
}

// Export replaces one bounded snapshot. It never touches flight logs and is run
// in a separate, low-priority systemd service, never in the copy process.
func Export(o Options) error {
	dir := filepath.Join(o.BootDir, "LOGFALCON-DIAGNOSTICS")
	if o.BootDir == "" {
		return fmt.Errorf("boot partition unavailable")
	}
	if free, err := util.FreeBytes(o.BootDir); err != nil {
		return err
	} else if free < 16*1024*1024 {
		return fmt.Errorf("boot partition has less than 16 MiB free; export skipped")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(o.StateDir, 0755); err != nil {
		return err
	}
	// Refuse overlapping timer, transfer and shutdown exports.
	lock, err := os.OpenFile(filepath.Join(o.StateDir, "export.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err = lockFile(lock); err != nil {
		return fmt.Errorf("diagnostic export already running")
	}
	defer unlockFile(lock)
	journal := o.Journal
	if journal == nil {
		journal = readJournal
	}
	snap := Snapshot{GeneratedUTC: time.Now().UTC(), BootID: BootID(), UptimeSec: Uptime(), Version: o.Version, Commit: o.Commit, Logs: map[string]string{}, Config: map[string]any{}, Warnings: []string{"A snapshot can lag the last event. Abrupt power loss can lose buffered logs. An incomplete lifecycle does not establish its cause."}}
	secrets := []string{"fpvpilot"}
	if o.Config != nil {
		c := o.Config
		secrets = append(secrets, c.HotspotPassword)
		snap.Config = map[string]any{"serial_baud": c.SerialBaud, "serial_timeout": c.SerialTimeout, "flash_chunk_size": c.FlashChunkSize, "erase_after_sync": c.EraseAfterSync, "storage_pressure_cleanup": c.StoragePressureCleanup, "min_free_space_mb": c.MinFreeSpaceMB, "debug_logging": c.DebugLogging, "web_port": c.WebPort, "credentials": "omitted"}
	} else {
		snap.Warnings = append(snap.Warnings, "Application configuration invalid or unavailable; no values exported.")
	}
	// Include secrets from the effective hotspot file even if application config is invalid.
	for _, path := range []string{"/etc/logfalcon/hostapd.conf", "/etc/hostapd/hostapd.conf"} {
		if b, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if value, ok := strings.CutPrefix(line, "wpa_passphrase="); ok {
					secrets = append(secrets, value)
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(o.StateDir, "boot-state.json")); err == nil {
		var state BootState
		if json.Unmarshal(b, &state) == nil {
			snap.Lifecycle = &state
		}
	}
	collect := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		b, err := journal(ctx, args...)
		if len(b) > 256*1024 {
			b = b[len(b)-256*1024:]
			snap.Warnings = append(snap.Warnings, "Journal section truncated to 256 KiB.")
		}
		if err != nil {
			snap.Warnings = append(snap.Warnings, "Journal section unavailable or timed out: "+strings.Join(args, " "))
		}
		return Redact(string(b), secrets...)
	}
	snap.Boots = collect("--list-boots", "-n", "2", "--no-pager", "--no-legend")
	for _, boot := range []string{"0", "-1"} {
		snap.Logs["services_boot_"+boot] = collect("--boot="+boot, "--no-pager", "--output=short-monotonic", "-n", "500", "-u", "logfalcon*", "-u", "hostapd", "-u", "dnsmasq", "-u", "systemd-networkd", "-u", "systemd-udevd")
		snap.Logs["kernel_boot_"+boot] = collect("--boot="+boot, "--kernel", "--no-pager", "--output=short-monotonic", "-n", "500")
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 2*1024*1024 {
		return fmt.Errorf("snapshot exceeds 2 MiB; previous snapshot retained")
	}
	path := filepath.Join(dir, "latest.json")
	// A process killed during the previous export may have left its fixed temp file.
	if err = atomicWrite(path, data, false); err != nil {
		return err
	}
	slog.Info("diagnostics exported", "path", path, "bytes", len(data))
	return nil
}

type boundedOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > 256*1024 {
		b.Reset()
		p = p[len(p)-256*1024:]
		b.truncated = true
	}
	if extra := b.Len() + len(p) - 256*1024; extra > 0 {
		b.Next(extra)
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func readJournal(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if out.truncated {
		return append([]byte("[journal section truncated]\n"), out.Bytes()...), err
	}
	return out.Bytes(), err
}

func atomicWrite(path string, data []byte, syncDir bool) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if syncDir {
		d, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		return d.Sync()
	}
	// FAT does not consistently support directory fsync; the file itself was synced.
	return nil
}

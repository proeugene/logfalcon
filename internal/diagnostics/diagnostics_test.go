package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/proeugene/logfalcon/internal/config"
)

func TestDebugLoggingHasContextAndCanBeDisabled(t *testing.T) {
	var out bytes.Buffer
	logger := newLogger(&out, false, "test", "abc")
	logger.Debug("hidden")
	if out.Len() != 0 {
		t.Fatal("debug enabled despite explicitly disabling it")
	}
	logger = newLogger(&out, true, "test", "abc").With("transfer_id", "copy-1")
	logger.Debug("protocol metadata", "code", 71, "payload_bytes", 7)
	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"time", "version", "commit", "boot_id", "uptime_sec", "pid", "transfer_id", "code", "payload_bytes"} {
		if _, ok := event[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}
}

func TestJournalPipeCopyHonoursCaptureLimit(t *testing.T) {
	const limit = 256 * 1024
	var out boundedOutput
	data := strings.Repeat("x", limit) + "end"
	if _, err := io.Copy(&out, io.LimitReader(strings.NewReader(data), int64(len(data)))); err != nil {
		t.Fatal(err)
	}
	if !out.truncated || out.buf.Len() != limit || !bytes.HasSuffix(out.buf.Bytes(), []byte("end")) {
		t.Fatal("journal pipe copy bypassed the capture limit")
	}
}

func TestReadJournalRetainsOutputAndFailureReason(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'retained output\\n'\nprintf 'journalctl: permission denied\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	b, err := readJournal(context.Background(), "--dmesg")
	if string(b) != "retained output\n" || err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("%q %v", b, err)
	}
	marker := filepath.Join(dir, "ready")
	t.Setenv("JOURNAL_TEST_READY", marker)
	script = "#!/bin/sh\nprintf 'before cancellation\\n'\n: > \"$JOURNAL_TEST_READY\"\nexec /bin/sleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() {
		b, err = readJournal(ctx)
		close(finished)
	}()
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		select {
		case <-finished:
			t.Fatalf("fixture exited before writing output: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-finished
	if string(b) != "before cancellation\n" || err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("%q %v", b, err)
	}
}

func TestExportPartialSectionsHaveSpecificReasons(t *testing.T) {
	o := Options{BootDir: t.TempDir(), StateDir: t.TempDir(), Journal: func(ctx context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "--kernel") || strings.Contains(joined, "--no-legend") {
			t.Fatal("unsupported Bookworm journal argument")
		}
		if strings.Contains(joined, "--list-boots") {
			// A known secret straddles the exported warning's 512-byte boundary.
			return []byte("0 boot-id"), errors.New(strings.Repeat(".", 508) + "fpvpilot")
		}
		if strings.Contains(joined, "--dmesg") {
			if strings.Contains(joined, "--boot=-1") {
				return []byte("partial kernel evidence"), errors.New("journalctl: Permission denied; password=secret-value")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []byte("useful services"), nil
	}}
	started := time.Now()
	if err := Export(o); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("section timeout not bounded")
	}
	b, err := os.ReadFile(filepath.Join(o.BootDir, "LOGFALCON-DIAGNOSTICS", "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s Snapshot
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Logs["services_boot_0"] != "useful services" || s.Logs["kernel_boot_-1"] != "partial kernel evidence" || s.Boots == "" {
		t.Fatal("partial evidence lost")
	}
	warnings := strings.Join(s.Warnings, " ")
	for _, want := range []string{"kernel_boot_0", "timed out after 2s", "kernel_boot_-1", "Permission denied"} {
		if !strings.Contains(warnings, want) {
			t.Fatal(warnings)
		}
	}
	if strings.Contains(warnings, "secret-value") || strings.Contains(warnings, "fpvp") {
		t.Fatal("stderr secret exported")
	}
}

func TestLifecycleRecognizesIncompleteRun(t *testing.T) {
	dir := t.TempDir()
	if err := Lifecycle(dir, "start", "first"); err != nil {
		t.Fatal(err)
	}
	if err := Lifecycle(dir, "start", "second"); err != nil {
		t.Fatal(err)
	}
	read := func() BootState {
		b, err := os.ReadFile(filepath.Join(dir, "boot-state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var s BootState
		if err = json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := read(); !s.PreviousRunIncomplete || s.PreviousBootID != "first" {
		t.Fatal(s)
	}
	if err := Lifecycle(dir, "stop", "second"); err != nil {
		t.Fatal(err)
	}
	if err := Lifecycle(dir, "start", "third"); err != nil {
		t.Fatal(err)
	}
	if read().PreviousRunIncomplete {
		t.Fatal("clean service stop marked incomplete")
	}
}

func TestExportIsBoundedRedactedAndReplacesSnapshot(t *testing.T) {
	cfg := config.Default()
	cfg.HotspotPassword = "secret-sentinel-123"
	o := Options{BootDir: t.TempDir(), StateDir: t.TempDir(), Version: "test", Commit: "abc", Config: cfg, Journal: func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("[1.2] usb connected\n[2.3] secret-sentinel-123\n[3.4] password=other-secret\n"), nil
	}}
	for i := 0; i < 3; i++ {
		if err := Export(o); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(o.BootDir, "LOGFALCON-DIAGNOSTICS")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("retention: %v %v", entries, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), cfg.HotspotPassword) || strings.Contains(string(b), "other-secret") {
		t.Fatal("secret exported")
	}
	var s Snapshot
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != "test" || s.Commit != "abc" || s.GeneratedUTC.IsZero() || len(s.Logs) != 4 {
		t.Fatal("missing context")
	}
	// Missing journal history is explicit; it must not prevent collection of other evidence.
	o.Journal = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("journal unavailable") }
	if err := Export(o); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "latest.json"))
	if !strings.Contains(string(b), "Journal section unavailable") {
		t.Fatal("missing journal failure hidden")
	}
	o.Journal = func(context.Context, ...string) ([]byte, error) { return []byte(strings.Repeat("x", 3*1024*1024)), nil }
	if err := Export(o); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "latest.json"))
	if err != nil || info.Size() > 2*1024*1024 {
		t.Fatal("snapshot unbounded", err)
	}
}

func TestExportFailurePreservesPrevious(t *testing.T) {
	o := Options{BootDir: t.TempDir(), StateDir: t.TempDir(), Journal: func(context.Context, ...string) ([]byte, error) { return []byte("ok"), nil }}
	if err := Export(o); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.BootDir, "LOGFALCON-DIAGNOSTICS", "latest.json")
	before, _ := os.ReadFile(path)
	// The fixed temp path cannot be opened; an existing complete snapshot survives.
	if err := os.Mkdir(path+".tmp", 0755); err != nil {
		t.Fatal(err)
	}
	if err := Export(o); err == nil {
		t.Fatal("write failure hidden")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("previous snapshot lost")
	}
}

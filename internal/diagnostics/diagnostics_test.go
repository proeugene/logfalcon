package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

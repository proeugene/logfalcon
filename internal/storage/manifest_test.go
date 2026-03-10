package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFCInfo() *FCInfo {
	return &FCInfo{
		APIMajor:       1,
		APIMinor:       46,
		Variant:        "BTFL",
		UID:            "abc1234567890def",
		BlackboxDevice: 1,
	}
}

func TestMakeSessionDir(t *testing.T) {
	root := t.TempDir()
	info := testFCInfo()

	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatalf("MakeSessionDir: %v", err)
	}

	// Verify the path is under root.
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}

	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 {
		t.Fatalf("expected 2 path components, got %d: %v", len(parts), parts)
	}

	// FC directory: fc_BTFL_uid-abc12345
	if !strings.HasPrefix(parts[0], "fc_BTFL_uid-abc12345") {
		t.Errorf("fc dir = %q, want prefix fc_BTFL_uid-abc12345", parts[0])
	}

	// Session directory: YYYY-MM-DD_HHMMSS
	today := time.Now().Format("2006-01-02")
	if !strings.HasPrefix(parts[1], today) {
		t.Errorf("session dir = %q, want prefix %s", parts[1], today)
	}

	// Verify the directory exists.
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("expected directory")
	}
}

func TestWriteManifest(t *testing.T) {
	root := t.TempDir()
	info := testFCInfo()

	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatalf("MakeSessionDir: %v", err)
	}

	timing := map[string]float64{"download_s": 12.5, "erase_s": 3.2}
	err = WriteManifest(dir, info, "deadbeef01234567", 1024*1024, false, false, timing)
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Read back.
	data, err := os.ReadFile(filepath.Join(dir, ManifestFilename))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if m.FC.Variant != "BTFL" {
		t.Errorf("variant = %q, want BTFL", m.FC.Variant)
	}
	if m.FC.UID != "abc1234567890def" {
		t.Errorf("uid = %q, want abc1234567890def", m.FC.UID)
	}
	if m.FC.APIVersion != "1.46" {
		t.Errorf("api_version = %q, want 1.46", m.FC.APIVersion)
	}
	if m.FC.BlackboxDevice != 1 {
		t.Errorf("blackbox_device = %d, want 1", m.FC.BlackboxDevice)
	}
	if m.File.Name != RawFlashFilename {
		t.Errorf("file name = %q, want %s", m.File.Name, RawFlashFilename)
	}
	if m.File.SHA256 != "deadbeef01234567" {
		t.Errorf("sha256 = %q, want deadbeef01234567", m.File.SHA256)
	}
	if m.File.Bytes != 1024*1024 {
		t.Errorf("bytes = %d, want %d", m.File.Bytes, 1024*1024)
	}
	if m.EraseAttempted {
		t.Error("erase_attempted should be false")
	}
	if m.EraseCompleted {
		t.Error("erase_completed should be false")
	}
	if m.Timing["download_s"] != 12.5 {
		t.Errorf("timing download_s = %v, want 12.5", m.Timing["download_s"])
	}
}

func TestUpdateManifestErase(t *testing.T) {
	root := t.TempDir()
	info := testFCInfo()

	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatalf("MakeSessionDir: %v", err)
	}

	// Write initial manifest.
	err = WriteManifest(dir, info, "aabbccdd", 512, false, false, nil)
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Update erase fields.
	timing := map[string]float64{"erase_s": 5.1}
	err = UpdateManifestErase(dir, true, timing)
	if err != nil {
		t.Fatalf("UpdateManifestErase: %v", err)
	}

	// Read back.
	data, err := os.ReadFile(filepath.Join(dir, ManifestFilename))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !m.EraseAttempted {
		t.Error("erase_attempted should be true")
	}
	if !m.EraseCompleted {
		t.Error("erase_completed should be true")
	}
	if m.Timing["erase_s"] != 5.1 {
		t.Errorf("timing erase_s = %v, want 5.1", m.Timing["erase_s"])
	}
	// Original fields should be preserved.
	if m.File.SHA256 != "aabbccdd" {
		t.Errorf("sha256 = %q, want aabbccdd", m.File.SHA256)
	}
}

func TestListSessions(t *testing.T) {
	root := t.TempDir()
	info := testFCInfo()

	// Build deterministic session dirs manually.
	uidShort := info.UID[:8]
	fcDirName := "fc_" + info.Variant + "_uid-" + uidShort
	fcDir := filepath.Join(root, fcDirName)
	timestamps := []string{"2024-06-01_100000", "2024-06-02_120000", "2024-06-03_140000"}
	for i, ts := range timestamps {
		d := filepath.Join(fcDir, ts)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		err := WriteManifest(d, info, "hash"+string(rune('0'+i)), int64(i*100), false, false, nil)
		if err != nil {
			t.Fatalf("WriteManifest: %v", err)
		}
	}

	sessions, err := ListSessions(root)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}

	// Newest first.
	if !strings.Contains(sessions[0].SessionDir, "2024-06-03") {
		t.Errorf("first session = %q, want newest (2024-06-03)", sessions[0].SessionDir)
	}
	if !strings.Contains(sessions[2].SessionDir, "2024-06-01") {
		t.Errorf("last session = %q, want oldest (2024-06-01)", sessions[2].SessionDir)
	}

	// BBLPath should be nil (no .bbl file created).
	for _, s := range sessions {
		if s.BBLPath != nil {
			t.Errorf("session %s: bbl_path should be nil, got %v", s.SessionID, *s.BBLPath)
		}
	}
}

func TestListSessionsEmpty(t *testing.T) {
	root := t.TempDir()
	sessions, err := ListSessions(root)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

func TestCleanupNotNeeded(t *testing.T) {
	root := t.TempDir()
	info := testFCInfo()

	// Create a session.
	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatalf("MakeSessionDir: %v", err)
	}
	err = WriteManifest(dir, info, "abcd", 256, false, false, nil)
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	// Request 1 byte free — should already be satisfied, nothing deleted.
	deleted, err := CleanupOldestSessions(root, 1)
	if err != nil {
		t.Fatalf("CleanupOldestSessions: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted %v, expected nothing", deleted)
	}

	// Session should still exist.
	sessions, err := ListSessions(root)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("got %d sessions, want 1", len(sessions))
	}
}

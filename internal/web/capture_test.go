package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

func TestCaptureCheckOnDashboardPollAndRecording(t *testing.T) {
	s, root, id := annotationFixture(t)
	data := []byte("original flight log bytes")
	headers := []storage.RecordingHeader{{Offset: 0}, {Offset: 12, Gyro: &storage.GyroFields{Filtered: 5}}}
	ref := strings.Split(id, "/")[0] + "/reference"
	check := storage.CaptureCheck{
		SessionID: id, Bytes: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Recordings: headers,
		Comparison: storage.CaptureComparison{Outcome: storage.CaptureAppended, ReferenceSessionID: ref, ReferenceBytes: 12},
	}
	m := storage.Manifest{Version: 1, FC: storage.ManifestFC{Variant: "BTFL", UID: "123456780000000000000000"},
		File: storage.ManifestFile{Name: storage.RawFlashFilename, Bytes: check.Bytes, SHA256: check.SHA256}, Recordings: headers}
	writeJSON := func(path string, v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(filepath.Join(root, id, storage.ManifestFilename), m)
	if err := os.WriteFile(filepath.Join(root, id, storage.RawFlashFilename), data, 0600); err != nil {
		t.Fatal(err)
	}
	lfSync.ConfigureStatus(s.config.StatusPath)
	t.Cleanup(func() { lfSync.ConfigureStatus("") })
	if err := lfSync.BeginTransfer("fake"); err != nil {
		t.Fatal(err)
	}
	status := lfSync.GetStatus()
	status.State = "complete"
	status.SavedCopy = &lfSync.SavedCopy{SessionID: id, ControllerUID: m.FC.UID, Bytes: check.Bytes, ControllerDataRetained: true}
	writeJSON(s.config.StatusPath, status)
	get := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s returned %d", path, w.Code)
		}
		return w.Body.String()
	}
	// A mandatory manifest can predate the check or belong to a legacy session.
	if !strings.Contains(get(recordingURL(id)), "Channel check unavailable.") {
		t.Fatal("missing check was presented as channel evidence")
	}
	if err := storage.WriteCaptureCheck(root, check); err != nil {
		t.Fatal(err)
	}
	poll := func() (capturePanel, transferPresentation) {
		t.Helper()
		var response struct {
			Capture      capturePanel
			Presentation transferPresentation
		}
		if err := json.Unmarshal([]byte(get("/status")), &response); err != nil {
			t.Fatal(err)
		}
		return response.Capture, response.Presentation
	}
	panel, transfer := poll()
	if panel.Summary != "Recording header 2 is new." || panel.Channels != "Filtered gyro on X, Z. No unfiltered gyro fields." ||
		panel.ReferenceHref != recordingURL(ref) || panel.DetailsHref != recordingURL(id) {
		t.Fatalf("poll lost capture evidence: %+v", panel)
	}
	s.configPath = filepath.Join(t.TempDir(), "logfalcon.toml")
	if err := os.WriteFile(s.configPath, []byte("setup_completed = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	page := get("/")
	if !strings.Contains(page, capturePanelHTML(panel)) || !strings.Contains(page, `href="`+esc(transfer.ActionHref)+`">Download saved .bbl`) {
		t.Fatal("initial page and polled capture/download differ")
	}
	// applyStatus(initialStatus) immediately replaces the server-rendered
	// content. Its JSON must contain the same evidence as the polling endpoint.
	_, initial, ok := strings.Cut(page, "const initialStatus=")
	raw, _, _ := strings.Cut(initial, ";const csrfToken=")
	var initialState struct{ Capture capturePanel }
	if !ok || json.Unmarshal([]byte(raw), &initialState) != nil || initialState.Capture != panel {
		t.Fatal("dashboard startup script lost the server-rendered capture evidence")
	}
	details := get(panel.DetailsHref)
	for _, want := range []string{esc(ref), `href="` + esc(panel.ReferenceHref) + `"`, "Gyro fields unknown.", "<strong>New.</strong> " + panel.Channels} {
		if !strings.Contains(details, want) {
			t.Errorf("missing recording detail %q", want)
		}
	}
	if !bytes.Equal([]byte(get(transfer.ActionHref)), data) {
		t.Fatal("capture result changed the primary download")
	}
	// Polling is bound to the exact published session, even if another session
	// from this controller is the only one on disk with a check.
	status.SavedCopy.SessionID = ref
	writeJSON(s.config.StatusPath, status)
	if other, _ := poll(); other.Summary != "Channel check unavailable." || other.DetailsHref != "" || other.ReferenceHref != "" {
		t.Fatalf("another session's result leaked into completion: %+v", other)
	}
	status.SavedCopy.SessionID = id
	writeJSON(s.config.StatusPath, status)
	check.Comparison.ReferenceSessionID = "../outside"
	if err := storage.WriteCaptureCheck(root, check); err != nil {
		t.Fatal(err)
	}
	if invalid, current := poll(); invalid.Summary != "Channel check unavailable." || invalid.ReferenceHref != "" || current.ActionHref != transfer.ActionHref {
		t.Fatalf("invalid optional reference leaked into presentation: %+v", invalid)
	}
}

func TestCapturePanelUncertainAndFirstCapture(t *testing.T) {
	check := storage.CaptureCheck{
		SessionID: "fc/copy", Recordings: []storage.RecordingHeader{{Gyro: &storage.GyroFields{Filtered: 7, Unfiltered: 7}}},
		Comparison: storage.CaptureComparison{Outcome: storage.CaptureUnavailable},
	}
	if p := presentCapture(&check); p.Summary != "No comparison available." ||
		p.Channels != "Header 1. Filtered gyro on X, Y, Z. Unfiltered gyro on X, Y, Z." || p.ReferenceHref != "" {
		t.Fatalf("first capture lost header fields: %+v", p)
	}
	check.Comparison = storage.CaptureComparison{Outcome: storage.CaptureAppended, ReferenceSessionID: "fc/ref", ReferenceBytes: 10, IndexLimited: true}
	check.Recordings = append(check.Recordings, storage.RecordingHeader{Offset: 10, Gyro: &storage.GyroFields{Filtered: 1}})
	p := presentCapture(&check)
	if !strings.Contains(p.Summary, "Total unconfirmed.") || !strings.Contains(p.Summary, "Header index limit reached.") || !strings.Contains(p.Channels, "on X.") {
		t.Fatalf("partial index presented an exact count or lost known fields: %+v", p)
	}
}

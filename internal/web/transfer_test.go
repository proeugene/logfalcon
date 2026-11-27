package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

func TestCompletedPanelIdentifiesExactSavedRecording(t *testing.T) {
	id := "fc_BTFL_uid-12345678/2026-11-10_120000-new-copy"
	for _, tc := range []struct {
		name, outcome               string
		retained, attempted, erased bool
	}{
		{"retained", "Data retained on controller.", true, false, false},
		{"erased", "Controller flash erased.", false, true, true},
		{"unknown", "Controller retention is unconfirmed.", false, false, false},
		{"erase-unconfirmed", "Controller erasure is unconfirmed.", false, true, false},
		{"contradictory", "Controller erasure is unconfirmed.", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := lfSync.Status{State: "complete", Progress: 100, SavedCopy: &lfSync.SavedCopy{
				SessionID: id, ControllerUID: "12345678<controller>", Bytes: 32 << 20,
				ControllerDataRetained: tc.retained, EraseAttempted: tc.attempted, EraseCompleted: tc.erased,
			}}
			page := RenderIndex(IndexParams{Status: status})
			if !strings.Contains(page, `href="/download/`+id+`/raw_flash.bbl">Download saved .bbl</a>`) {
				t.Fatal("completion action does not identify the exact saved recording")
			}
			if !strings.Contains(page, "Controller 12345678&lt;controller&gt; · 32.0 MB saved. "+tc.outcome) || strings.Contains(page, "12345678<controller>") {
				t.Fatal("saved controller, size or retention evidence missing/unsafe")
			}
		})
	}
}

func TestFailedCopyUsesStableReasonWithoutClaimingCompletion(t *testing.T) {
	for code, want := range transferNextActions {
		status := lfSync.Status{State: "error", ReasonCode: code, SavedCopy: &lfSync.SavedCopy{SessionID: "fc/session", Bytes: 1024}}
		p := presentTransfer(status)
		if p.Body != want || p.Tone == "complete" || p.Step == 4 || strings.HasPrefix(p.ActionHref, "/download/") {
			t.Fatalf("%s hides uncertainty or lacks the next action: %+v", code, p)
		}
	}
	for _, status := range []lfSync.Status{
		{State: "error", ReasonCode: "new-unknown-code"},
		{State: "unavailable", ReasonCode: "flash_busy"},
		{State: "interrupted", SavedCopy: &lfSync.SavedCopy{SessionID: "fc/session", Bytes: 1024}},
	} {
		p := presentTransfer(status)
		if p.Body != presentation(status.State).Body || p.Tone == "complete" {
			t.Fatalf("unconfirmed outcome became success/actionable without evidence: %+v", p)
		}
	}
}

func TestOlderCompletionDoesNotGuessSavedRecording(t *testing.T) {
	for _, copy := range []*lfSync.SavedCopy{nil, {Bytes: 100}, {SessionID: "fc/session", Bytes: -1}, {SessionID: "../session", Bytes: 3}} {
		p := presentTransfer(lfSync.Status{State: "complete", SavedCopy: copy})
		if p.ActionHref != "#saved-logs" || strings.Contains(p.Body, "Controller ") {
			t.Fatalf("older/incomplete status guessed a saved recording: %+v", p)
		}
	}
}

func TestPilotDownloadsExactCopyFromCompletionPanel(t *testing.T) {
	s, _, id := annotationFixture(t)
	view := presentTransfer(lfSync.Status{State: "complete", SavedCopy: &lfSync.SavedCopy{SessionID: id, Bytes: 3}})
	if !strings.Contains(view.Body, "3 B saved") || !strings.Contains(RenderSessions(s.getSessions()), "3 B · .BBL") {
		t.Fatal("a short non-empty recording is shown as zero bytes")
	}
	response := httptest.NewRecorder()
	s.ServeHTTP(response, httptest.NewRequest("GET", view.ActionHref, nil))
	if response.Code != 200 || response.Body.String() != "bbl" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("completion action %q returned %d %q instead of the saved BBL", view.ActionHref, response.Code, response.Header().Get("Content-Type"))
	}
}

func TestStatusPollingCarriesSameResultAsInitialPage(t *testing.T) {
	s, _ := newTestServer(t)
	lfSync.ConfigureStatus(s.config.StatusPath)
	t.Cleanup(func() { lfSync.ConfigureStatus("") })
	if err := lfSync.BeginTransfer("test"); err != nil {
		t.Fatal(err)
	}
	status := lfSync.GetStatus()
	status.State = "complete"
	status.SavedCopy = &lfSync.SavedCopy{SessionID: "fc_BTFL_uid-12345678/session", ControllerUID: "12345678", Bytes: 2 << 20, ControllerDataRetained: true}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.config.StatusPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
	var result struct {
		SavedCopy    *lfSync.SavedCopy    `json:"saved_copy"`
		Presentation transferPresentation `json:"presentation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SavedCopy == nil || *result.SavedCopy != *status.SavedCopy || result.Presentation != presentTransfer(status) {
		t.Fatalf("poll lost exact saved-copy context: %s", w.Body.String())
	}
	page := RenderIndex(IndexParams{Status: status})
	if !strings.Contains(page, esc(result.Presentation.Body)) || !strings.Contains(page, `href="`+esc(result.Presentation.ActionHref)+`"`) {
		t.Fatal("initial page and polled result disagree")
	}
}

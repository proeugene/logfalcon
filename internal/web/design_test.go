package web

import (
	"github.com/proeugene/logfalcon/internal/storage"
	lf "github.com/proeugene/logfalcon/internal/sync"
	"strings"
	"testing"
)

func TestStatusPresentationDoesNotConfirmUnknownResults(t *testing.T) {
	for _, state := range []string{"waiting", "interrupted", "error", "unavailable", "unknown"} {
		p := presentation(state)
		if p.Step == 4 || p.Tone == "complete" || p.Active {
			t.Fatalf("%s presented as active or completed: %+v", state, p)
		}
	}
	p := presentation("complete")
	if p.Step != 4 || p.Tone != "complete" {
		t.Fatal("confirmed completion lost")
	}
}
func TestDashboardEscapesStatusAndCompatibilityWarning(t *testing.T) {
	page := RenderIndex(IndexParams{Status: lf.Status{State: "syncing", Progress: 150, Message: "</script><script>alert(1)</script>", Warning: "Firmware <too new>"}})
	if strings.Contains(page, "<script>alert(1)</script>") || !strings.Contains(page, "Firmware &lt;too new&gt;") || !strings.Contains(page, `aria-valuenow="100"`) {
		t.Fatal("unsafe status rendering, hidden compatibility warning, or unbounded progress")
	}
}
func TestRecordingsOrderAndAvailabilityWithoutMutatingCache(t *testing.T) {
	path := "file"
	older := &storage.Session{SessionID: "older", SessionDir: "2026-10-20_100000", BBLPath: &path}
	newer := &storage.Session{SessionID: "newer", SessionDir: "2026-10-21_100000", BBLPath: &path}
	missing := &storage.Session{SessionID: "missing", SessionDir: "2026-10-22_100000"}
	sessions := []*storage.Session{older, missing, newer}
	page := RenderSessions(sessions)
	if sessions[0] != older || sessions[1] != missing {
		t.Fatal("shared session cache reordered")
	}
	newerIndex, olderIndex := strings.Index(page, `data-session="newer"`), strings.Index(page, `data-session="older"`)
	if newerIndex < 0 || olderIndex < 0 || newerIndex > olderIndex || strings.Contains(page, `href="/download/missing/raw_flash.bbl"`) {
		t.Fatal("recordings are out of order or an unavailable file is downloadable")
	}
}

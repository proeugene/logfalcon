package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

func TestIdleShutdownTerminalStatesAndUserActivity(t *testing.T) {
	for _, state := range []string{"complete", "error", "empty", "interrupted", "waiting", "syncing", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.config.IdleShutdownMinutes = 1
			// Use this process's real boot identity through BeginTransfer.
			lfSync.ConfigureStatus(s.config.StatusPath)
			t.Cleanup(func() { lfSync.ConfigureStatus("") })
			if err := lfSync.BeginTransfer("test"); err != nil {
				t.Fatal(err)
			}
			lfSync.SetStatus(state, 0, "test")
			s.lastTransfer = lfSync.GetStatus().TransferID
			now := time.Now()
			s.lastActivity = now.Add(-2 * time.Minute)
			called := false
			s.shutdown = func() error { called = true; return nil }
			if state == "syncing" || state == "unavailable" {
				if s.checkIdleShutdown(now) || called {
					t.Fatal("active/unknown state powered off")
				}
				return
			}
			// Polling does not extend the idle window, a real page visit does.
			s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/status", nil))
			if !s.checkIdleShutdown(now) || !called {
				t.Fatal("terminal result prevented shutdown")
			}
			_ = os.Remove(filepath.Join(filepath.Dir(s.config.StatusPath), "shutdown.pending"))
			called = false
			s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			if s.checkIdleShutdown(time.Now()) || called {
				t.Fatal("page visit ignored")
			}
			s.lastActivity = now.Add(-2 * time.Minute)
			s.activeRequests = 1
			if s.checkIdleShutdown(now) || called {
				t.Fatal("in-progress download ignored")
			}
		})
	}
}

func TestIdleShutdownFailureCanRetry(t *testing.T) {
	s, _ := newTestServer(t)
	s.config.IdleShutdownMinutes = 1
	s.lastActivity = time.Now().Add(-2 * time.Minute)
	s.shutdown = func() error { return errors.New("poweroff failed") }
	if s.checkIdleShutdown(time.Now()) {
		t.Fatal("failed shutdown treated as success")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.config.StatusPath), "shutdown.pending")); !os.IsNotExist(err) {
		t.Fatal("failed shutdown blocks future copies")
	}
}

type brokenStream struct {
	*httptest.ResponseRecorder
	writes int
}

func (w *brokenStream) Write(b []byte) (int, error) {
	w.writes++
	return 0, errors.New("client disconnected")
}
func TestSSEInitialStatusAndDisconnect(t *testing.T) {
	s, _ := newTestServer(t)
	w := &brokenStream{ResponseRecorder: httptest.NewRecorder()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/events", nil).WithContext(ctx))
	if w.writes != 1 {
		t.Fatalf("stream kept writing after disconnect: %d", w.writes)
	}
}

func TestDownloadHeadAndConditional(t *testing.T) {
	s, root := newTestServer(t)
	dir := filepath.Join(root, "fc_test", "session")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"bytes":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	url := "/download/fc_test/session/manifest.json"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("HEAD", url, nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("ETag") == "" {
		t.Fatal("invalid HEAD response")
	}
	r := httptest.NewRequest("GET", url, nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatal("conditional response ignored")
	}
}

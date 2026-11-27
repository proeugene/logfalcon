package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// deadlineResponse models the absolute response deadline installed by net/http,
// with a short initial limit so a progressing slow download is testable quickly.
type deadlineResponse struct {
	*httptest.ResponseRecorder
	deadline        time.Time
	renewals        int
	invalidDeadline bool
}

func (w *deadlineResponse) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.renewals++
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > time.Minute {
		w.invalidDeadline = true
	}
	return nil
}

func (w *deadlineResponse) Write(p []byte) (int, error) {
	time.Sleep(20 * time.Millisecond)
	if time.Now().After(w.deadline) {
		return 0, os.ErrDeadlineExceeded
	}
	return w.ResponseRecorder.Write(p)
}

func TestIndividualDownloadOutlivesInitialWriteDeadline(t *testing.T) {
	s, root := newTestServer(t)
	dir := filepath.Join(root, "fc_test", "session")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a saved blackbox byte sequence\n"), 16<<10)
	if err := os.WriteFile(filepath.Join(dir, "raw_flash.bbl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	w := &deadlineResponse{ResponseRecorder: httptest.NewRecorder(), deadline: time.Now().Add(50 * time.Millisecond)}
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/download/fc_test/session/raw_flash.bbl", nil))
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("progressing download cut off by initial deadline: got %d of %d bytes", w.Body.Len(), len(data))
	}
	if w.renewals == 0 || w.invalidDeadline {
		t.Fatalf("download must renew a bounded deadline, renewals=%d invalid=%t", w.renewals, w.invalidDeadline)
	}
}

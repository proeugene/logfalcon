package web

import (
	"encoding/json"
	"mime"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proeugene/logfalcon/internal/storage"
)

func TestDistinctStableBBLNamesAndLegacyDownloads(t *testing.T) {
	root := t.TempDir()
	ids := []string{"fc_test/2026-11-11_120000-aaa", "fc_test/2026-11-11_120000-bbb", "fc_old/2026-11-11_120000"}
	data := "H Product:Blackbox flight data recorder by Nicholas Sherlock\nH Craft name:DARKSTAR20\n\x00original bytes"
	names := map[string]bool{}
	for i, id := range ids {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, storage.RawFlashFilename), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			b, _ := json.Marshal(storage.Manifest{File: storage.ManifestFile{Bytes: int64(len(data))}, Recordings: []storage.RecordingHeader{{CraftName: "DARKSTAR20\"/\r\n"}}})
			if err := os.WriteFile(filepath.Join(dir, storage.ManifestFilename), b, 0644); err != nil {
				t.Fatal(err)
			}
		}
		s := &Server{storagePath: root}
		getName := func() string {
			t.Helper()
			w := httptest.NewRecorder()
			s.handleDownload(w, httptest.NewRequest("GET", "/download/"+id+"/raw_flash.bbl", nil))
			kind, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if w.Code != 200 || w.Body.String() != data || err != nil || kind != "attachment" {
				t.Fatalf("download changed: %d %s", w.Code, w.Header())
			}
			return params["filename"]
		}
		name := getName()
		if names[name] || name != getName() || !strings.HasPrefix(name, "DARKSTAR20_") || strings.ContainsAny(name, "/\r\n\"") {
			t.Fatal(name)
		}
		names[name] = true
	}
	if name := bblDownloadName("fc_older/session", nil); !strings.HasPrefix(name, "LogFalcon_") {
		t.Fatal(name)
	}
}

func TestFlightTimesSeparateFromCollection(t *testing.T) {
	s := &storage.Session{SessionDir: "2026-11-11_120000", Manifest: &storage.Manifest{Recordings: []storage.RecordingHeader{{Offset: 0, StartTime: "2026-11-10T10:11:12+05:30"}, {Offset: 1024}}}}
	text := recordingTimes(s)
	if !strings.Contains(text, "2026-11-10T10:11:12+05:30") || !strings.Contains(text, "1 of 2") || strings.Contains(text, "2026-11-11") {
		t.Fatal(text)
	}
	if text = recordingTimes(&storage.Session{}); !strings.Contains(text, "unknown") {
		t.Fatal(text)
	}
}

func TestIndividualDownloadChecksSavedLengthAtRequestTime(t *testing.T) {
	s, root := newTestServer(t)
	id := "fc_test/session"
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, storage.RawFlashFilename)
	if err := os.WriteFile(path, []byte("full copy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, storage.ManifestFilename), []byte(`{"file":{"name":"raw_flash.bbl","bytes":9}}`), 0644); err != nil {
		t.Fatal(err)
	}
	fragment := httptest.NewRecorder()
	s.ServeHTTP(fragment, httptest.NewRequest("GET", "/sessions/fragment", nil))
	link := "/download/" + id + "/raw_flash.bbl"
	if !strings.Contains(fragment.Body.String(), link) {
		t.Fatal("fixture did not offer a download")
	}
	if err := os.Truncate(path, 3); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code != 409 || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("truncated log offered as a download: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code != 404 {
		t.Fatalf("missing log: HTTP %d", w.Code)
	}
}

func TestLegacyCollectionTimeUsesManifestTimezone(t *testing.T) {
	s := &storage.Session{SessionDir: "2026-07-01_180000", Manifest: &storage.Manifest{CreatedUTC: "2026-07-01T17:01:12Z"}}
	if got := sessionDate(s); got != "1 Jul 2026 · 17:01 UTC" {
		t.Fatalf("legacy Pi local directory time presented as UTC: %s", got)
	}
	s.Manifest.CreatedUTC = ""
	if got := sessionDate(s); !strings.Contains(got, "18:00") || strings.Contains(got, "UTC") {
		t.Fatalf("legacy session without a known timezone: %s", got)
	}
}

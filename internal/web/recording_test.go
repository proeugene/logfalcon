package web

import (
	"github.com/proeugene/logfalcon/internal/storage"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func annotationFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s, root := newTestServer(t)
	id := "fc_BTFL_uid-12345678/2026-10-21_142300"
	dir := filepath.Join(root, filepath.FromSlash(id))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"manifest.json": `{"version":1,"file":{"bytes":3}}`, "raw_flash.bbl": "bbl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return s, root, id
}
func annotationRequest(s *Server, path string, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestPilotAnnotationsRoundTripAndEvidencePreserved(t *testing.T) {
	s, root, id := annotationFixture(t)
	before, err := os.ReadFile(filepath.Join(root, id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	v := url.Values{"csrf_token": {s.csrfToken}, "session": {id}, "title": {"New props <test>"}, "notes": {"  Vibration on punch-outs.\n第二次 test <script>alert(1)</script>  "}}
	w := annotationRequest(s, "/recording", v)
	if w.Code != 303 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	s2 := NewServer(root, s.config)
	w = httptest.NewRecorder()
	s2.ServeHTTP(w, httptest.NewRequest("GET", recordingURL(id), nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "New props &lt;test&gt;") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("reopened annotations not safely rendered")
	}
	a, err := storage.ReadRecordingAnnotation(root, id)
	if err != nil || a.Notes != v.Get("notes") {
		t.Fatal("notes changed or lost")
	}
	fragment := httptest.NewRecorder()
	s.ServeHTTP(fragment, httptest.NewRequest("GET", "/sessions/fragment", nil))
	if !strings.Contains(fragment.Body.String(), "New props &lt;test&gt;") || strings.Contains(fragment.Body.String(), "<script>alert(1)</script>") || !strings.Contains(fragment.Body.String(), "Collected at") {
		t.Fatal("dashboard context missing, unsafe or labelled as flight time")
	}
	after, _ := os.ReadFile(filepath.Join(root, id, "manifest.json"))
	data, _ := os.ReadFile(filepath.Join(root, id, "raw_flash.bbl"))
	if string(before) != string(after) || string(data) != "bbl" {
		t.Fatal("transfer evidence modified")
	}
	v.Set("title", "")
	v.Set("notes", "")
	if w = annotationRequest(s, "/recording", v); w.Code != 303 {
		t.Fatal("clear annotations failed")
	}
}
func TestControllerNicknameSharedAcrossRecordings(t *testing.T) {
	s, root, id := annotationFixture(t)
	other := "fc_BTFL_uid-12345678/2026-10-22_100000"
	if err := os.Mkdir(filepath.Join(root, other), 0755); err != nil {
		t.Fatal(err)
	}
	v := url.Values{"csrf_token": {s.csrfToken}, "session": {id}, "nickname": {"5-inch freestyle"}}
	w := annotationRequest(s, "/recording/controller", v)
	if w.Code != 303 {
		t.Fatalf("nickname %d", w.Code)
	}
	c, err := storage.ReadControllerAnnotation(root, other)
	if err != nil || c.Nickname != "5-inch freestyle" {
		t.Fatal("nickname not shared")
	}
	a, _ := storage.ReadRecordingAnnotation(root, id)
	if a.Title != "" || a.Notes != "" {
		t.Fatal("nickname changed recording notes")
	}
}
func TestAnnotationFormsRejectInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name, path, id, token, title, notes, nickname string
		want                                          int
	}{{name: "csrf", path: "/recording", token: "stale", want: 403}, {name: "title", path: "/recording", title: strings.Repeat("x", 121), want: 400}, {name: "notes", path: "/recording", notes: strings.Repeat("x", 2001), want: 400}, {name: "control", path: "/recording", title: "bad\x00title", want: 400}, {name: "nickname", path: "/recording/controller", nickname: strings.Repeat("x", 81), want: 400}, {name: "traversal", path: "/recording", id: "../outside", want: 404}} {
		t.Run(tc.name, func(t *testing.T) {
			s, root, id := annotationFixture(t)
			token := s.csrfToken
			if tc.token != "" {
				token = tc.token
			}
			if tc.id != "" {
				id = tc.id
			}
			w := annotationRequest(s, tc.path, url.Values{"csrf_token": {token}, "session": {id}, "title": {tc.title}, "notes": {tc.notes}, "nickname": {tc.nickname}})
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
			files, _ := filepath.Glob(filepath.Join(root, "fc_*", "*", "pilot-notes.json"))
			if len(files) != 0 {
				t.Fatal("invalid request saved notes")
			}
		})
	}
}
func TestAnnotationWriteFailureAndOlderRecording(t *testing.T) {
	s, root, id := annotationFixture(t)
	sessions, err := storage.ListSessions(root)
	if err != nil || len(sessions) != 1 || sessions[0].AnnotationUnavailable {
		t.Fatal("older session requires annotations")
	}
	target := filepath.Join(root, id, "pilot-notes.json")
	if err = os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	w := annotationRequest(s, "/recording", url.Values{"csrf_token": {s.csrfToken}, "session": {id}, "title": {"keep me"}})
	if w.Code != 503 || !strings.Contains(w.Body.String(), "Could not confirm") {
		t.Fatal("failed save reported success")
	}
	st, err := os.Stat(target)
	if err != nil || !st.IsDir() {
		t.Fatal("failed save overwrote existing evidence")
	}
}

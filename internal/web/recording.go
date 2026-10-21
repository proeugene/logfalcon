package web

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/proeugene/logfalcon/internal/storage"
)

func recordingURL(id string) string { return "/recording?session=" + url.QueryEscape(id) }

func (s *Server) recordingExists(id string) bool {
	f, err := storage.OpenSessionFile(s.storagePath, id, storage.ManifestFilename)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func (s *Server) handleRecordingGet(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session")
	if !s.recordingExists(id) {
		s.sendError(w, r, 404, "Saved recording not found.")
		return
	}
	a, err := storage.ReadRecordingAnnotation(s.storagePath, id)
	c, cerr := storage.ReadControllerAnnotation(s.storagePath, id)
	if err != nil || cerr != nil {
		s.sendError(w, r, 503, "Pilot annotations could not be read. Existing files have been preserved; inspect storage before editing.")
		return
	}
	msg := ""
	switch r.URL.Query().Get("saved") {
	case "recording":
		msg = "Notes saved."
	case "controller":
		msg = "Controller name saved."
	}
	s.sendHTML(w, r, 200, RenderRecording(id, s.Version, s.csrfToken, msg, a, c))
}

func (s *Server) annotationForm(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		s.sendError(w, r, 400, "Invalid or oversized annotation form. Nothing was saved.")
		return "", false
	}
	if r.PostForm.Get("csrf_token") != s.csrfToken {
		s.sendError(w, r, 403, "This page expired. Reload the recording before saving.")
		return "", false
	}
	id := r.PostForm.Get("session")
	if !s.recordingExists(id) {
		s.sendError(w, r, 404, "Saved recording not found. Nothing was saved.")
		return "", false
	}
	return id, true
}
func (s *Server) handleRecordingPost(w http.ResponseWriter, r *http.Request) {
	id, ok := s.annotationForm(w, r)
	if !ok {
		return
	}
	a := storage.RecordingAnnotation{Title: r.PostForm.Get("title"), Notes: r.PostForm.Get("notes")}
	if err := a.Validate(); err != nil {
		s.sendError(w, r, 400, err.Error())
		return
	}
	if err := storage.SaveRecordingAnnotation(s.storagePath, id, a); err != nil {
		s.sendError(w, r, 503, "Could not confirm the annotation save. Your recording and manifest are unchanged. Reopen this page to check the saved notes before retrying.")
		return
	}
	s.invalidateSessionsCache()
	http.Redirect(w, r, recordingURL(id)+"&saved=recording", http.StatusSeeOther)
}
func (s *Server) handleControllerPost(w http.ResponseWriter, r *http.Request) {
	id, ok := s.annotationForm(w, r)
	if !ok {
		return
	}
	a := storage.ControllerAnnotation{Nickname: r.PostForm.Get("nickname")}
	if err := a.Validate(); err != nil {
		s.sendError(w, r, 400, err.Error())
		return
	}
	if err := storage.SaveControllerAnnotation(s.storagePath, id, a); err != nil {
		s.sendError(w, r, 503, "Could not confirm the nickname save. Reopen this page to check the saved name before retrying. Recording data is unchanged.")
		return
	}
	s.invalidateSessionsCache()
	http.Redirect(w, r, recordingURL(id)+"&saved=controller", http.StatusSeeOther)
}

func RenderRecording(id, version, csrf, msg string, a storage.RecordingAnnotation, c storage.ControllerAnnotation) string {
	body := pageIntro("", "Recording details", "") + `<div class="editor">`
	if msg != "" {
		body += `<p class="msg-success" role="status">` + esc(msg) + `</p>`
	}
	hidden := `<input type="hidden" name="csrf_token" value="` + esc(csrf) + `"><input type="hidden" name="session" value="` + esc(id) + `">`
	body += `<form class="panel form-panel" method="post" action="/recording">` + hidden + `<p class="field-note">Title and notes are optional.</p><div class="form-group"><label for="recording-title">Title</label><input id="recording-title" type="text" name="title" maxlength="120" value="` + esc(a.Title) + `"></div><div class="form-group"><label for="recording-notes">Notes</label><textarea id="recording-notes" name="notes" maxlength="2000" rows="5">` + esc(a.Notes) + `</textarea><small class="field-hint">Up to 2,000 characters.</small></div><details class="technical"><summary>File location</summary>` + esc(id) + `</details><div class="form-actions"><button class="primary">Save notes</button><a class="btn secondary" href="/#saved-logs">Back</a></div></form><form class="panel form-panel controller-form" method="post" action="/recording/controller">` + hidden + `<h2>Controller name</h2><p class="field-note">Used for all recordings from this controller.</p><div class="form-group"><label for="controller-nickname">Nickname</label><input type="text" id="controller-nickname" name="nickname" maxlength="80" value="` + esc(c.Nickname) + `"><small class="field-hint">Optional. Leave blank to show the controller ID.</small></div><button class="secondary">Save name</button></form></div>`
	return renderShell("Recording details", "dashboard", version, body, recordingEditorJS)
}

const recordingEditorJS = `
const forms=Array.from(document.querySelectorAll('main form'));
const contents=f=>new URLSearchParams(new FormData(f)).toString();
const original=new Map(forms.map(f=>[f,contents(f)]));
let submitting=false;
forms.forEach(form=>form.addEventListener('submit',e=>{
 if(forms.some(f=>f!==form&&contents(f)!==original.get(f))&&!confirm('The other section has unsaved edits. Save this section and discard those other edits?')){e.preventDefault();return;}
 submitting=true;form.querySelector('button').disabled=true;form.querySelector('button').textContent='Saving…';
}));
window.addEventListener('beforeunload',e=>{if(!submitting&&forms.some(f=>contents(f)!==original.get(f))){e.preventDefault();e.returnValue='';}});
`

func recordingTitle(s *storage.Session) string {
	if strings.TrimSpace(s.Annotation.Title) != "" {
		return s.Annotation.Title
	}
	return "Controller flash copy"
}
func recordingController(s *storage.Session) string {
	if strings.TrimSpace(s.Nickname) != "" {
		return fmt.Sprintf("%s · %s", s.Nickname, controllerLabel(s.FCDir))
	}
	return controllerLabel(s.FCDir)
}

func recordingContext(s *storage.Session) string {
	var b strings.Builder
	b.WriteString(`<div class="recording-context">`)
	b.WriteString(`<p>Collected at ` + esc(sessionDate(s)) + `</p>`)
	notes := []rune(s.Annotation.Notes)
	if len(notes) > 180 {
		notes = append(notes[:180], '…')
	}
	if len(notes) > 0 {
		b.WriteString(`<p>` + esc(string(notes)) + `</p>`)
	}
	if s.AnnotationUnavailable {
		b.WriteString(`<p>Pilot annotations unavailable. Recording data is unchanged.</p>`)
	}
	b.WriteString(`<a class="btn secondary" href="` + esc(recordingURL(s.SessionID)) + `">Edit details</a></div>`)
	return b.String()
}

func recordingRevision(s *storage.Session) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%q%q%q%t%t", s.Annotation.Title, s.Annotation.Notes, s.Nickname, s.AnnotationUnavailable, s.BBLPath != nil))))
}

func recordingCheckbox(s *storage.Session) string {
	disabled := ""
	if s.BBLPath == nil {
		disabled = " disabled"
	}
	return `<label class="recording-select-wrap"><input type="checkbox" class="recording-select" form="export-form" name="session" value="` + esc(s.SessionID) + `" aria-label="Select recording collected at ` + esc(sessionDate(s)) + `"` + disabled + `></label>`
}

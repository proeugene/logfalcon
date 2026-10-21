package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pilot annotations are kept separate from transfer evidence.
type RecordingAnnotation struct {
	Title string `json:"title"`
	Notes string `json:"notes"`
}

type ControllerAnnotation struct {
	Nickname string `json:"nickname"`
}

func validAnnotationText(s string, limit int, multiline bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && (!multiline || (r != '\n' && r != '\r' && r != '\t')) {
			return false
		}
	}
	return true
}

func (a RecordingAnnotation) Validate() error {
	if !validAnnotationText(a.Title, 120, false) || !validAnnotationText(a.Notes, 2000, true) {
		return fmt.Errorf("use a title up to 120 characters and notes up to 2,000 characters; control characters are not allowed")
	}
	return nil
}
func (a ControllerAnnotation) Validate() error {
	if !validAnnotationText(a.Nickname, 80, false) {
		return fmt.Errorf("use a controller nickname up to 80 characters without control characters")
	}
	return nil
}

func readAnnotation(root *os.Root, name string, value any) error {
	st, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("annotation must be a regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("annotation changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return err
	}
	if len(b) > 16384 {
		return fmt.Errorf("annotation exceeds size limit")
	}
	return json.Unmarshal(b, value)
}

func controllerAnnotationRoot(path, sessionID string) (*os.Root, error) {
	// Validate and pin the session before deriving its controller directory.
	session, err := SessionRoot(path, sessionID)
	if err != nil {
		return nil, err
	}
	_ = session.Close()
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	fc := strings.Split(sessionID, "/")[0]
	st, err := root.Lstat(fc)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("controller must be a real directory")
	}
	return root.OpenRoot(fc)
}

func ReadRecordingAnnotation(path, id string) (RecordingAnnotation, error) {
	var a RecordingAnnotation
	root, err := SessionRoot(path, id)
	if err != nil {
		return a, err
	}
	defer func() { _ = root.Close() }()
	err = readAnnotation(root, "pilot-notes.json", &a)
	if err == nil {
		err = a.Validate()
	}
	return a, err
}
func SaveRecordingAnnotation(path, id string, a RecordingAnnotation) error {
	if err := a.Validate(); err != nil {
		return err
	}
	root, err := SessionRoot(path, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var old RecordingAnnotation
	if err = readAnnotation(root, "pilot-notes.json", &old); err != nil {
		return err
	}
	if err = old.Validate(); err != nil {
		return err
	}
	return atomicJSONWrite(root, "pilot-notes.json", a)
}
func ReadControllerAnnotation(path, id string) (ControllerAnnotation, error) {
	var a ControllerAnnotation
	root, err := controllerAnnotationRoot(path, id)
	if err != nil {
		return a, err
	}
	defer func() { _ = root.Close() }()
	err = readAnnotation(root, "pilot-controller.json", &a)
	if err == nil {
		err = a.Validate()
	}
	return a, err
}
func SaveControllerAnnotation(path, id string, a ControllerAnnotation) error {
	if err := a.Validate(); err != nil {
		return err
	}
	root, err := controllerAnnotationRoot(path, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var old ControllerAnnotation
	if err = readAnnotation(root, "pilot-controller.json", &old); err != nil {
		return err
	}
	if err = old.Validate(); err != nil {
		return err
	}
	return atomicJSONWrite(root, "pilot-controller.json", a)
}

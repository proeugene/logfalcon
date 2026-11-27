package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/proeugene/logfalcon/internal/msp"
)

// CaptureReference identifies a retained copy by the evidence published with it.
type CaptureReference struct {
	SessionID string `json:"session_id"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

const (
	CaptureUnavailable = "unavailable"
	CaptureIdentical   = "identical"
	CaptureAppended    = "appended"
	CaptureChanged     = "changed"
)

type CaptureComparison struct {
	Outcome            string `json:"outcome"`
	ReferenceSessionID string `json:"reference_session_id,omitempty"`
	ReferenceBytes     int64  `json:"reference_bytes,omitempty"`
	IndexLimited       bool   `json:"index_limited,omitempty"`
}

// IsNewHeader is true only for a corroborated header in a verified extension.
// A Product marker alone can be left behind by an interrupted recording.
func (c CaptureComparison) IsNewHeader(h RecordingHeader) bool {
	return c.Outcome == CaptureAppended && h.Offset >= c.ReferenceBytes && h.Gyro != nil
}

func CompareCapture(ref *CaptureReference, size int64, fullSHA, prefixSHA string, headers []RecordingHeader, limited bool) CaptureComparison {
	c := CaptureComparison{Outcome: CaptureUnavailable, IndexLimited: limited}
	if ref == nil || ref.SessionID == "" || ref.Bytes <= 0 || size <= 0 ||
		!lowerHex(ref.SHA256, sha256.Size) || !lowerHex(fullSHA, sha256.Size) {
		return c
	}
	c.ReferenceSessionID = ref.SessionID
	c.ReferenceBytes = ref.Bytes
	c.Outcome = CaptureChanged
	if size == ref.Bytes && fullSHA == ref.SHA256 {
		c.Outcome = CaptureIdentical
		return c
	}
	if size <= ref.Bytes || prefixSHA != ref.SHA256 {
		return c
	}
	for _, h := range headers {
		if h.Offset >= ref.Bytes && h.Offset < size && h.Gyro != nil {
			c.Outcome = CaptureAppended
			break
		}
	}
	return c
}

func lowerHex(s string, bytes int) bool {
	if len(s) != 2*bytes || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func captureReferenceName(uid string) (string, error) {
	// MSP UID is three 32-bit words. Unset or placeholder identities cannot
	// distinguish two controllers, even when they are formatted as hex.
	if !lowerHex(uid, 12) || uid == "000000000000000000000000" || uid == "ffffffffffffffffffffffff" {
		return "", errors.New("a full Betaflight controller UID is required")
	}
	return ".capture-reference-" + uid + ".json", nil
}

// Only bounded regular files can supply optional capture evidence. Limit the
// read as well as checking its size because the file can grow after stat.
func readCaptureJSON(root *os.Root, name string, limit int64, dst any) error {
	entry, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() || entry.Size() > limit {
		return errors.New("saved metadata must be a bounded regular file")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !os.SameFile(entry, st) {
		return errors.New("saved metadata changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return errors.New("saved metadata exceeds size limit")
	}
	return json.Unmarshal(data, dst)
}

// ReadManifest reads a single bounded manifest from a regular, storage-rooted file.
func ReadManifest(storagePath, sessionID string) (*Manifest, error) {
	session, err := SessionRoot(storagePath, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	var m Manifest
	if err := readCaptureJSON(session, ManifestFilename, 1<<20, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// The full UID is already validated by the caller when deriving the pointer name.
func retainedCapture(storagePath, uid, sessionID string) (*CaptureReference, error) {
	controller, _, ok := strings.Cut(sessionID, "/")
	if !ok || controller != "fc_BTFL_uid-"+uid[:8] {
		return nil, errors.New("capture reference controller directory mismatch")
	}
	session, err := SessionRoot(storagePath, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	var m Manifest
	if err := readCaptureJSON(session, ManifestFilename, 1<<20, &m); err != nil {
		return nil, err
	}
	if m.Version != 1 || m.FC.UID != uid || m.FC.Variant != "BTFL" || m.FC.BlackboxDevice != msp.BlackboxDeviceFlash || !m.ControllerDataRetained ||
		m.EraseAttempted || m.EraseCompleted || m.File.Name != RawFlashFilename || m.File.Bytes <= 0 ||
		!lowerHex(m.File.SHA256, sha256.Size) {
		return nil, errors.New("capture reference lacks a retained Betaflight copy for this controller")
	}
	st, err := session.Lstat(RawFlashFilename)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() != m.File.Bytes {
		return nil, errors.New("capture reference BBL differs from manifest")
	}
	return &CaptureReference{SessionID: sessionID, Bytes: m.File.Bytes, SHA256: m.File.SHA256}, nil
}

// LoadCaptureReference returns nil without an error when there is no pointer.
// Reconfirm the target on every collection, since cleanup may have removed it.
func LoadCaptureReference(storagePath, uid string) (*CaptureReference, error) {
	name, err := captureReferenceName(uid)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(storagePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var recorded CaptureReference
	err = readCaptureJSON(root, name, 1024, &recorded)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	verified, err := retainedCapture(storagePath, uid, recorded.SessionID)
	if err != nil {
		return nil, err
	}
	if *verified != recorded {
		return nil, errors.New("capture reference differs from saved manifest")
	}
	return verified, nil
}

// StoreCaptureReference and RetireCaptureReference run under the collection lock.
func StoreCaptureReference(storagePath, uid, sessionID string) error {
	name, err := captureReferenceName(uid)
	if err != nil {
		return err
	}
	ref, err := retainedCapture(storagePath, uid, sessionID)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(storagePath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return atomicJSONWrite(root, name, ref)
}

func RetireCaptureReference(storagePath, uid string) error {
	name, err := captureReferenceName(uid)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(storagePath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.Remove(name); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("retire capture reference: %w", err)
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

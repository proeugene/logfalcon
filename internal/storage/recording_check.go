package storage

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

const captureFilename = "capture-check.json"

// CaptureCheck is optional, per-session evidence. It lives outside the manifest
// so failure to publish or read it cannot affect copy verification or erasure.
type CaptureCheck struct {
	SessionID  string            `json:"session_id"`
	Bytes      int64             `json:"bytes"`
	SHA256     string            `json:"sha256"`
	Comparison CaptureComparison `json:"comparison"`
	Recordings []RecordingHeader `json:"recordings,omitempty"`
}

func WriteCaptureCheck(storagePath string, check CaptureCheck) error {
	root, err := SessionRoot(storagePath, check.SessionID)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	// Craft names and times are already in the manifest, in the same order.
	headers := make([]RecordingHeader, len(check.Recordings))
	for i, r := range check.Recordings {
		headers[i] = RecordingHeader{Offset: r.Offset, Gyro: r.Gyro}
	}
	check.Recordings = headers
	return atomicJSONWrite(root, captureFilename, check)
}

// ReadCaptureCheck pins the session and checks that the evidence belongs to
// its published manifest. Dashboard polls do not open or hash the flash dump.
func ReadCaptureCheck(storagePath, sessionID string) (*CaptureCheck, error) {
	root, err := SessionRoot(storagePath, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var c CaptureCheck
	if err := readCaptureJSON(root, captureFilename, 64<<10, &c); err != nil {
		return nil, err
	}
	var m Manifest
	if err := readCaptureJSON(root, ManifestFilename, 1<<20, &m); err != nil {
		return nil, err
	}
	if !lowerHex(c.SHA256, sha256.Size) || c.SessionID != sessionID ||
		c.Bytes <= 0 || c.Bytes != m.File.Bytes || c.SHA256 != m.File.SHA256 ||
		len(c.Recordings) > maxRecordingHeaders || len(c.Recordings) != len(m.Recordings) {
		return nil, fmt.Errorf("capture check does not match the saved manifest")
	}
	var lastOffset int64 = -1
	for i, r := range c.Recordings {
		if r.Offset <= lastOffset || r.Offset >= c.Bytes || r.Offset != m.Recordings[i].Offset ||
			(r.Gyro != nil && (r.Gyro.Filtered > 7 || r.Gyro.Unfiltered > 7)) {
			return nil, fmt.Errorf("invalid recording evidence")
		}
		lastOffset = r.Offset
	}
	comparison := c.Comparison
	if comparison.Outcome == CaptureUnavailable {
		if comparison.ReferenceSessionID != "" || comparison.ReferenceBytes != 0 {
			return nil, fmt.Errorf("unexpected capture reference")
		}
		return &c, nil
	}
	ref := comparison.ReferenceSessionID
	if !fs.ValidPath(ref) || strings.ContainsAny(ref, `\:`) || strings.Count(ref, "/") != 1 ||
		path.Dir(ref) != path.Dir(sessionID) || ref == sessionID || comparison.ReferenceBytes <= 0 {
		return nil, fmt.Errorf("invalid comparison session")
	}
	switch comparison.Outcome {
	case CaptureIdentical:
		if comparison.ReferenceBytes == c.Bytes {
			return &c, nil
		}
	case CaptureChanged:
		return &c, nil
	case CaptureAppended:
		for _, r := range c.Recordings {
			if comparison.IsNewHeader(r) {
				return &c, nil
			}
		}
	}
	return nil, fmt.Errorf("unsupported or inconsistent capture comparison")
}

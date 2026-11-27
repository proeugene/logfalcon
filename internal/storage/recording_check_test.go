package storage

import "testing"

func TestReadCaptureCheckRequiresMatchingSavedCopy(t *testing.T) {
	root := t.TempDir()
	const raw = "saved dump"
	id := makeCaptureSession(t, root, captureUID, raw)
	check := CaptureCheck{SessionID: id, Bytes: int64(len(raw)), SHA256: captureDigest(raw), Comparison: CaptureComparison{Outcome: CaptureUnavailable}}
	if err := WriteCaptureCheck(root, check); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCaptureCheck(root, id); err != nil {
		t.Fatal(err)
	}
	// A different archive can share a controller, byte count or even all
	// file bytes. Persisted evidence must belong to this exact session.
	for _, tc := range []struct {
		name string
		edit func(*CaptureCheck)
	}{
		{"session", func(c *CaptureCheck) { c.SessionID = "fc_BTFL_uid-aabbccdd/other" }},
		{"size", func(c *CaptureCheck) { c.Bytes++ }},
		{"hash", func(c *CaptureCheck) { c.SHA256 = captureDigest("other dump") }},
		{"unexpected headers", func(c *CaptureCheck) { c.Recordings = []RecordingHeader{{Offset: 1}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := check
			tc.edit(&bad)
			dir, err := SessionRoot(root, id)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = dir.Close() }()
			if err := atomicJSONWrite(dir, captureFilename, bad); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadCaptureCheck(root, id); err == nil {
				t.Fatal("accepted mismatched sidecar")
			}
		})
	}
}

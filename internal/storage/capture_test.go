package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const captureUID = "aabbccdd1234567812345678"

func captureDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func makeCaptureSession(t *testing.T, root, uid, data string) string {
	t.Helper()
	info := &FCInfo{Variant: "BTFL", UID: uid, BlackboxDevice: 1}
	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, RawFlashFilename), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(root, dir, info, captureDigest(data), int64(len(data)), false, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := RecordRetained(root, dir); err != nil {
		t.Fatal(err)
	}
	id, err := filepath.Rel(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(id)
}

func TestCaptureReferencesSurviveRestartAndSeparateFullUIDs(t *testing.T) {
	root := t.TempDir()
	other := "aabbccdd8765432187654321"
	if ref, err := LoadCaptureReference(root, captureUID); err != nil || ref != nil {
		t.Fatalf("unexpected first reference: %v, %v", ref, err)
	}
	a := makeCaptureSession(t, root, captureUID, "quad A")
	b := makeCaptureSession(t, root, other, "quad B")
	if err := StoreCaptureReference(root, captureUID, a); err != nil {
		t.Fatal(err)
	}
	if err := StoreCaptureReference(root, other, b); err != nil {
		t.Fatal(err)
	}
	if err := StoreCaptureReference(root, captureUID, b); err == nil {
		t.Fatal("reference accepted a different controller with the same directory prefix")
	}
	for uid, id := range map[string]string{captureUID: a, other: b} {
		ref, err := LoadCaptureReference(root, uid)
		if err != nil || ref == nil || ref.SessionID != id {
			t.Fatalf("load %s: %v, %v", uid, ref, err)
		}
	}
	if err := RetireCaptureReference(root, other); err != nil {
		t.Fatal(err)
	}
	if ref, err := LoadCaptureReference(root, other); ref != nil || err != nil {
		t.Fatalf("retired reference: %v, %v", ref, err)
	}
	if ref, err := LoadCaptureReference(root, captureUID); ref == nil || ref.SessionID != a || err != nil {
		t.Fatalf("unrelated reference lost: %v, %v", ref, err)
	}
	if err := DeleteSession(root, a); err != nil {
		t.Fatal(err)
	}
	if ref, err := LoadCaptureReference(root, captureUID); ref != nil || err == nil {
		t.Fatalf("deleted session accepted: %v, %v", ref, err)
	}
}

func TestCaptureReferenceRequiresRetainedFilesAndDurableDigest(t *testing.T) {
	for _, change := range []string{"erase", "wrong-size", "symlink", "digest", "missing-retention", "bad-device"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			id := makeCaptureSession(t, root, captureUID, "saved log")
			if err := StoreCaptureReference(root, captureUID, id); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, filepath.FromSlash(id))
			bbl := filepath.Join(dir, RawFlashFilename)
			switch change {
			case "erase":
				if err := UpdateManifestErase(root, dir, false, nil); err != nil {
					t.Fatal(err)
				}
			case "wrong-size":
				if err := os.Truncate(bbl, 1); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(bbl, bbl+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(bbl+".original", bbl); err != nil {
					t.Fatal(err)
				}
			default:
				m, err := ReadManifest(root, id)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "digest":
					m.File.SHA256 = captureDigest("different")
				case "bad-device":
					m.FC.BlackboxDevice = 2
				case "missing-retention":
					m.ControllerDataRetained = false
				}
				data, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ManifestFilename), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if ref, err := LoadCaptureReference(root, captureUID); ref != nil || err == nil {
				t.Fatalf("accepted damaged reference: %v, %v", ref, err)
			}
		})
	}
}

func TestCaptureReferenceRejectsAmbiguousIdentityAndMalformedPointer(t *testing.T) {
	root := t.TempDir()
	for _, uid := range []string{"", "unknown", captureUID[:8], strings.ToUpper(captureUID), strings.Repeat("0", 24), strings.Repeat("f", 24), "../" + captureUID} {
		if ref, err := LoadCaptureReference(root, uid); ref != nil || err == nil {
			t.Fatalf("accepted ambiguous UID: %q", uid)
		}
	}
	id := makeCaptureSession(t, root, captureUID, "evidence")
	if err := StoreCaptureReference(root, captureUID, id); err != nil {
		t.Fatal(err)
	}
	name, err := captureReferenceName(captureUID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{string(good) + "x", string(good) + strings.Repeat(" ", 1025), `{"session_id":"fc_BTFL_uid-aabbccdd/.."}`, `{}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if ref, err := LoadCaptureReference(root, captureUID); ref != nil || err == nil {
			t.Fatalf("accepted malformed reference: %v, %v", ref, err)
		}
	}
	if err := os.WriteFile(path+".target", good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".target", path); err != nil {
		t.Fatal(err)
	}
	if ref, err := LoadCaptureReference(root, captureUID); ref != nil || err == nil {
		t.Fatalf("accepted symlinked reference: %v, %v", ref, err)
	}
}

func TestCompareCaptureRequiresAnExactPrefixAndCorroboratedHeader(t *testing.T) {
	old := "saved bytes"
	ref := &CaptureReference{SessionID: "fc_BTFL_uid-aabbccdd/previous", Bytes: int64(len(old)), SHA256: captureDigest(old)}
	start := ref.Bytes
	newHeader := RecordingHeader{Offset: start, Gyro: &GyroFields{Filtered: 7}}
	prefix := func(s string) string {
		if len(s) < len(old) {
			return ""
		}
		return captureDigest(s[:len(old)])
	}
	for _, tc := range []struct {
		name    string
		data    string
		headers []RecordingHeader
		limited bool
		want    string
	}{
		{"same even with capped index", old, nil, true, CaptureIdentical},
		{"shorter", old[:5], nil, false, CaptureChanged},
		{"same length rewritten", "Saved bytes", nil, false, CaptureChanged},
		{"overwritten prior padding", old[:len(old)-1] + "!+header", []RecordingHeader{newHeader}, false, CaptureChanged},
		{"continuation", old + " more bytes", nil, false, CaptureChanged},
		{"ambiguous marker", old + "header", []RecordingHeader{{Offset: start}}, false, CaptureChanged},
		{"header began in old bytes", old + "continued header", []RecordingHeader{{Offset: start - 1, Gyro: &GyroFields{}}}, false, CaptureChanged},
		{"append", old + "header", []RecordingHeader{newHeader}, false, CaptureAppended},
		{"append with limited index", old + "header", []RecordingHeader{newHeader}, true, CaptureAppended},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := CompareCapture(ref, int64(len(tc.data)), captureDigest(tc.data), prefix(tc.data), tc.headers, tc.limited)
			if c.Outcome != tc.want || c.ReferenceSessionID != ref.SessionID || c.ReferenceBytes != ref.Bytes || c.IndexLimited != tc.limited {
				t.Fatalf("comparison = %#v, want %s", c, tc.want)
			}
			if c.IsNewHeader(newHeader) != (tc.want == CaptureAppended) || c.IsNewHeader(RecordingHeader{Offset: start}) ||
				c.IsNewHeader(RecordingHeader{Offset: start - 1, Gyro: &GyroFields{}}) {
				t.Fatal("incorrect new-header identification")
			}
		})
	}
	first := CompareCapture(nil, int64(len(old)), captureDigest(old), "", nil, false)
	if first.Outcome != CaptureUnavailable || first.ReferenceSessionID != "" {
		t.Fatalf("unexpected first comparison: %#v", first)
	}
}

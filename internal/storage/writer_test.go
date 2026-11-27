package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamWriterBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "basic.bin")

	w, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}

	data := []byte("hello, betaflight!")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Fatalf("Write returned %d, want %d", n, len(data))
	}
	if w.BytesWritten() != int64(len(data)) {
		t.Fatalf("BytesWritten = %d, want %d", w.BytesWritten(), len(data))
	}

	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])
	if got := w.SHA256Hex(); got != expectedHex {
		t.Fatalf("SHA256Hex = %s, want %s", got, expectedHex)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(data)) {
		t.Fatalf("file size = %d, want %d", info.Size(), len(data))
	}
}

func TestStreamWriterVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.bin")

	w, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}

	data := []byte("verify me please")
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	match, fileHash, err := w.VerifyAgainstFile()
	if err != nil {
		t.Fatalf("VerifyAgainstFile: %v", err)
	}
	if !match {
		t.Fatalf("hash mismatch: writer=%s file=%s", w.SHA256Hex(), fileHash)
	}
	if fileHash != w.SHA256Hex() {
		t.Fatalf("fileHash=%s, want %s", fileHash, w.SHA256Hex())
	}
}

func TestStreamWriterAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abort.bin")

	w, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}

	if _, err := w.Write([]byte("partial data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, got err=%v", err)
	}
}

func TestStreamWriterEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")

	w, err := NewStreamWriter(path)
	if err != nil {
		t.Fatalf("NewStreamWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if w.BytesWritten() != 0 {
		t.Fatalf("BytesWritten = %d, want 0", w.BytesWritten())
	}

	// SHA-256 of empty input
	empty := sha256.Sum256(nil)
	expectedHex := hex.EncodeToString(empty[:])
	if got := w.SHA256Hex(); got != expectedHex {
		t.Fatalf("SHA256Hex = %s, want %s", got, expectedHex)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("file size = %d, want 0", info.Size())
	}
}

func TestCapturePrefixUsesOnlyWrittenBytes(t *testing.T) {
	const data = "abcdefghij"
	for _, length := range []int64{0, 1, 3, 4, 10, 11} {
		w, err := NewStreamWriter(filepath.Join(t.TempDir(), "capture.bbl"))
		if err != nil {
			t.Fatal(err)
		}
		w.SetCapturePrefixLength(length)
		for _, chunk := range []string{"abc", "defg", "hij"} {
			if _, err := w.Write([]byte(chunk)); err != nil {
				t.Fatal(err)
			}
			if w.BytesWritten() < length && w.CapturePrefixSHA256() != "" {
				t.Fatalf("published incomplete prefix at %d / %d", w.BytesWritten(), length)
			}
		}
		want := ""
		if length > 0 && length <= int64(len(data)) {
			sum := sha256.Sum256([]byte(data[:length]))
			want = hex.EncodeToString(sum[:])
		}
		if got := w.CapturePrefixSHA256(); got != want {
			t.Errorf("prefix length %d: got %q, want %q", length, got, want)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if ok, _, err := w.VerifyAgainstFile(); err != nil || !ok {
			t.Fatalf("prefix length %d changed whole-file verification: %v", length, err)
		}
	}
}

func TestCaptureIndexReportsLimit(t *testing.T) {
	w, err := NewStreamWriter(filepath.Join(t.TempDir(), "many.bbl"))
	if err != nil {
		t.Fatal(err)
	}
	const header = productHeader + "H Field I name:loopIteration,time,gyroADC[0]\n"
	if _, err := w.Write([]byte(strings.Repeat(header, maxRecordingHeaders-1))); err != nil {
		t.Fatal(err)
	}
	if w.CaptureIndexLimited() {
		t.Fatal("reported limit before reaching it")
	}
	if _, err := w.Write([]byte(header + header)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	headers := w.RecordingHeaders()
	if !w.CaptureIndexLimited() || len(headers) != maxRecordingHeaders || headers[len(headers)-1].Gyro.Filtered != 1 {
		t.Fatal("missing limited-index or channel evidence at cap")
	}
}

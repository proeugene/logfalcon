package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

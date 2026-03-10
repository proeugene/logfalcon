package storage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

const bufSize = 256 * 1024

// StreamWriter writes data to a file with buffering, tracks bytes written,
// and computes a running SHA-256 hash.
type StreamWriter struct {
	path         string
	file         *os.File
	buf          *bufio.Writer
	hasher       hash.Hash
	bytesWritten int64
}

// NewStreamWriter creates parent directories if needed, opens the file for
// writing, and returns a buffered StreamWriter.
func NewStreamWriter(path string) (*StreamWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create parent dirs: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	w := &StreamWriter{
		path:   path,
		file:   f,
		hasher: sha256.New(),
	}
	w.buf = bufio.NewWriterSize(f, bufSize)
	return w, nil
}

// Write implements io.Writer. Every byte is hashed and counted.
func (w *StreamWriter) Write(data []byte) (int, error) {
	n, err := w.buf.Write(data)
	if n > 0 {
		w.hasher.Write(data[:n])
		w.bytesWritten += int64(n)
	}
	return n, err
}

// Close flushes the buffer, fsyncs the file, and closes it.
func (w *StreamWriter) Close() error {
	if err := w.buf.Flush(); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("flush: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	return w.file.Close()
}

// Abort closes and removes the partial file.
func (w *StreamWriter) Abort() error {
	_ = w.file.Close()
	return os.Remove(w.path)
}

// BytesWritten returns the total number of bytes written so far.
func (w *StreamWriter) BytesWritten() int64 {
	return w.bytesWritten
}

// SHA256Hex returns the hex-encoded SHA-256 digest of all data written.
func (w *StreamWriter) SHA256Hex() string {
	return hex.EncodeToString(w.hasher.Sum(nil))
}

// VerifyAgainstFile re-reads the file on disk and compares its SHA-256 hash
// against the in-memory hash computed during writing.
// Returns (match, fileHash, err).
func (w *StreamWriter) VerifyAgainstFile() (bool, string, error) {
	f, err := os.Open(w.path)
	if err != nil {
		return false, "", fmt.Errorf("open for verify: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, "", fmt.Errorf("read for verify: %w", err)
	}
	fileHash := hex.EncodeToString(h.Sum(nil))
	return fileHash == w.SHA256Hex(), fileHash, nil
}

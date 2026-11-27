package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/proeugene/logfalcon/internal/storage"
)

// Check the manifest against the open download at request time. A card already
// displayed in the browser can outlive the file it described.
func downloadRecordings(root, sessionID string, bbl io.ReaderAt, size int64) ([]storage.RecordingHeader, error) {
	f, err := storage.OpenSessionFile(root, sessionID, storage.ManifestFilename)
	if err == nil {
		defer func() { _ = f.Close() }()
		const maxManifest = 1 << 20
		data, err := io.ReadAll(io.LimitReader(f, maxManifest+1))
		var m storage.Manifest
		if err != nil || len(data) > maxManifest || json.Unmarshal(data, &m) != nil {
			return nil, errors.New("saved manifest unreadable or oversized")
		}
		if m.File.Bytes != size {
			return nil, errors.New("log size differs from the saved manifest")
		}
		if len(m.Recordings) > 0 {
			return m.Recordings, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// Older files can lack recording metadata or a manifest. Read a bounded
	// prefix from the same handle without changing the download's position.
	return storage.ReadHeaderPrefix(io.NewSectionReader(bbl, 0, size)), nil
}

// bblDownloadName uses immutable header evidence and the full session identity.
// Collection time alone cannot distinguish copies when an offline clock repeats.
func bblDownloadName(sessionID string, recordings []storage.RecordingHeader) string {
	craft := "LogFalcon"
	for _, r := range recordings {
		if r.CraftName != "" {
			craft = r.CraftName
			break
		}
	}
	var safe strings.Builder
	for _, c := range craft {
		if safe.Len() >= 64 {
			break
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			safe.WriteRune(c)
		} else if safe.Len() > 0 && !strings.HasSuffix(safe.String(), "_") {
			safe.WriteByte('_')
		}
	}
	name := strings.Trim(safe.String(), "_-")
	if name == "" {
		name = "LogFalcon"
	}
	id := sha256.Sum256([]byte(sessionID))
	return name + "_" + hex.EncodeToString(id[:12]) + ".bbl"
}

// downloadResponse keeps a progressing download alive beyond the server's
// response limit while retaining a bounded deadline for a stalled connection.
// It deliberately exposes no ReaderFrom method: ServeContent's copies must pass
// through Write so both complete and ranged responses renew the deadline.
type downloadResponse struct {
	http.ResponseWriter
	controller *http.ResponseController
	renewed    time.Time
}

func newDownloadResponse(w http.ResponseWriter) *downloadResponse {
	return &downloadResponse{ResponseWriter: w, controller: http.NewResponseController(w)}
}

func (w *downloadResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *downloadResponse) Write(b []byte) (int, error) {
	if time.Since(w.renewed) > 10*time.Second {
		if err := w.controller.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return 0, err
		}
		w.renewed = time.Now()
	}
	return w.ResponseWriter.Write(b)
}

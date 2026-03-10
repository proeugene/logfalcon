package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/proeugene/logfalcon/internal/util"
)

const (
	ManifestFilename = "manifest.json"
	RawFlashFilename = "raw_flash.bbl"
)

// FCInfo mirrors fc.FCInfo to avoid circular imports.
type FCInfo struct {
	APIMajor       int
	APIMinor       int
	Variant        string // "BTFL" or "INAV"
	UID            string // hex string
	BlackboxDevice int
}

// Manifest describes a single download session persisted as manifest.json.
type Manifest struct {
	Version        int                `json:"version"`
	CreatedUTC     string             `json:"created_utc"`
	FC             ManifestFC         `json:"fc"`
	File           ManifestFile       `json:"file"`
	EraseAttempted bool               `json:"erase_attempted"`
	EraseCompleted bool               `json:"erase_completed"`
	Timing         map[string]float64 `json:"timing,omitempty"`
}

// ManifestFC holds flight-controller metadata inside a manifest.
type ManifestFC struct {
	Variant        string `json:"variant"`
	UID            string `json:"uid"`
	APIVersion     string `json:"api_version"`
	BlackboxDevice int    `json:"blackbox_device"`
}

// ManifestFile holds file metadata inside a manifest.
type ManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Session represents a single download session discovered on disk.
type Session struct {
	SessionID  string    `json:"session_id"`
	FCDir      string    `json:"fc_dir"`
	SessionDir string    `json:"session_dir"`
	Path       string    `json:"path"`
	BBLPath    *string   `json:"bbl_path"`
	Manifest   *Manifest `json:"manifest"`
}

// atomicJSONWrite writes data as indented JSON to path with fsync for durability.
func atomicJSONWrite(path string, data any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	return f.Close()
}

// MakeSessionDir creates a timestamped session directory.
// Layout: <root>/fc_<VARIANT>_uid-<uid8>/<YYYY-MM-DD_HHMMSS>/
func MakeSessionDir(root string, info *FCInfo) (string, error) {
	uidShort := "unknown"
	if info.UID != "" && info.UID != "unknown" {
		if len(info.UID) > 8 {
			uidShort = info.UID[:8]
		} else {
			uidShort = info.UID
		}
	}
	fcDir := filepath.Join(root, fmt.Sprintf("fc_%s_uid-%s", info.Variant, uidShort))
	timestamp := time.Now().Format("2006-01-02_150405")
	sessionDir := filepath.Join(fcDir, timestamp)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	return sessionDir, nil
}

// WriteManifest writes manifest.json to the session directory.
func WriteManifest(dir string, info *FCInfo, sha256hex string, usedSize int64,
	eraseCompleted, eraseAttempted bool, timing map[string]float64) error {

	m := Manifest{
		Version:    1,
		CreatedUTC: time.Now().UTC().Format(time.RFC3339),
		FC: ManifestFC{
			Variant:        info.Variant,
			UID:            info.UID,
			APIVersion:     fmt.Sprintf("%d.%d", info.APIMajor, info.APIMinor),
			BlackboxDevice: info.BlackboxDevice,
		},
		File: ManifestFile{
			Name:   RawFlashFilename,
			SHA256: sha256hex,
			Bytes:  usedSize,
		},
		EraseAttempted: eraseAttempted,
		EraseCompleted: eraseCompleted,
		Timing:         timing,
	}
	return atomicJSONWrite(filepath.Join(dir, ManifestFilename), m)
}

// UpdateManifestErase updates erase fields in an existing manifest.
func UpdateManifestErase(dir string, eraseCompleted bool, timing map[string]float64) error {
	path := filepath.Join(dir, ManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	m.EraseAttempted = true
	m.EraseCompleted = eraseCompleted
	if timing != nil {
		m.Timing = timing
	}
	return atomicJSONWrite(path, m)
}

// ListSessions returns all sessions under root, newest first.
func ListSessions(root string) ([]*Session, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read root dir: %w", err)
	}

	var sessions []*Session
	for _, fcEntry := range entries {
		if !fcEntry.IsDir() {
			continue
		}
		fcDirName := fcEntry.Name()
		fcDirPath := filepath.Join(root, fcDirName)
		sessionEntries, err := os.ReadDir(fcDirPath)
		if err != nil {
			continue
		}
		for _, sessEntry := range sessionEntries {
			if !sessEntry.IsDir() {
				continue
			}
			sessDirName := sessEntry.Name()
			sessDirPath := filepath.Join(fcDirPath, sessDirName)
			manifestPath := filepath.Join(sessDirPath, ManifestFilename)
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				continue
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				continue // skip corrupted manifests
			}
			bblPath := filepath.Join(sessDirPath, RawFlashFilename)
			var bblPtr *string
			if _, err := os.Stat(bblPath); err == nil {
				bblPtr = &bblPath
			}
			sessions = append(sessions, &Session{
				SessionID:  fmt.Sprintf("%s/%s", fcDirName, sessDirName),
				FCDir:      fcDirName,
				SessionDir: sessDirName,
				Path:       sessDirPath,
				BBLPath:    bblPtr,
				Manifest:   &m,
			})
		}
	}

	// Sort newest first by session directory name (timestamp-based).
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].SessionDir > sessions[j].SessionDir
	})
	return sessions, nil
}

// CleanupOldestSessions deletes oldest sessions until requiredFreeBytes is
// available. Returns deleted session IDs.
func CleanupOldestSessions(root string, requiredFreeBytes int64) ([]string, error) {
	if requiredFreeBytes <= 0 {
		return nil, nil
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	// Collect all session directories, sorted oldest first.
	type candidate struct {
		sessionID  string
		sessionDir string
	}
	var candidates []candidate

	fcEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read root dir: %w", err)
	}
	for _, fcEntry := range fcEntries {
		if !fcEntry.IsDir() {
			continue
		}
		fcDirPath := filepath.Join(root, fcEntry.Name())
		sessEntries, err := os.ReadDir(fcDirPath)
		if err != nil {
			continue
		}
		for _, sessEntry := range sessEntries {
			if !sessEntry.IsDir() {
				continue
			}
			candidates = append(candidates, candidate{
				sessionID:  fmt.Sprintf("%s/%s", fcEntry.Name(), sessEntry.Name()),
				sessionDir: filepath.Join(fcDirPath, sessEntry.Name()),
			})
		}
	}

	// Sort oldest first (ascending by session directory name).
	sort.Slice(candidates, func(i, j int) bool {
		return filepath.Base(candidates[i].sessionDir) < filepath.Base(candidates[j].sessionDir)
	})

	var deleted []string
	for _, c := range candidates {
		free, err := util.FreeBytes(root)
		if err != nil {
			return deleted, fmt.Errorf("check free space: %w", err)
		}
		if free >= requiredFreeBytes {
			break
		}
		if err := os.RemoveAll(c.sessionDir); err != nil {
			return deleted, fmt.Errorf("remove session %s: %w", c.sessionID, err)
		}
		// Remove empty FC directory.
		parent := filepath.Dir(c.sessionDir)
		remaining, _ := os.ReadDir(parent)
		if len(remaining) == 0 {
			_ = os.Remove(parent)
		}
		deleted = append(deleted, c.sessionID)
	}
	return deleted, nil
}

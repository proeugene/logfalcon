// Package settings serialises browser and boot configuration changes. A durable
// undo journal makes a process interruption recoverable on the next invocation.
package settings

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
	"github.com/proeugene/logfalcon/internal/config"
)

type Values struct {
	SSID, Password, Country, Debug string // empty means preserve
	SetupCompleted                 bool
	DeviceIdentity                 string
}

type Manager struct {
	Hostapd, Config string
	LegacyHash      string                     // optional migration-path override for filesystem tests
	Restart         func() error               // must wait for completion; nil only before hostapd starts
	Write           func(string, []byte) error // optional failure injection for tests
}

type backup struct {
	Data   []byte
	Exists bool
}
type journal struct{ Hostapd, Config, Hash backup }

func (m Manager) path(name string) string { return filepath.Join(filepath.Dir(m.Config), name) }
func (m Manager) write(path string, data []byte) error {
	if m.Write != nil {
		return m.Write(path, data)
	}
	return AtomicWrite(path, data)
}

// AtomicWrite durably replaces a file, preserving its owner (also when root
// applies boot settings on behalf of the unprivileged web service).
func AtomicWrite(path string, data []byte) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		info, err = os.Stat(filepath.Dir(path))
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	defer func() { _ = f.Close() }()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		if err = f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func snapshot(path string) (backup, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return backup{}, err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return backup{}, nil
	}
	if err != nil {
		return backup{}, err
	}
	if !info.Mode().IsRegular() {
		return backup{}, fmt.Errorf("configuration must be a regular file")
	}
	b, err := root.ReadFile(name)
	return backup{Data: b, Exists: err == nil}, err
}

func (m Manager) locked(fn func() error) error {
	f, err := os.OpenFile(m.path("settings.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("settings lock must be a regular file")
	}
	// Boot runs as root; the browser must subsequently open the same lock.
	if os.Geteuid() == 0 {
		info, err := os.Stat(filepath.Dir(m.Config))
		if err != nil {
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if err = f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
				return err
			}
		}
	}
	// Fail promptly rather than queue changes based on stale browser state.
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("settings busy; retry: %w", err)
	}
	if err = m.recover(); err != nil {
		return err
	}
	return fn()
}

// Recover must run before the web service accepts requests or hostapd starts.
func (m Manager) Recover() error { return m.locked(func() error { return nil }) }

func (m Manager) recover() error {
	b, err := os.ReadFile(m.path("settings-undo.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var j journal
	if err = json.Unmarshal(b, &j); err != nil {
		return fmt.Errorf("settings recovery journal invalid")
	}
	for _, item := range []struct {
		path string
		old  backup
	}{{m.Hostapd, j.Hostapd}, {m.Config, j.Config}, {m.path("boot-applied.sha256"), j.Hash}} {
		if item.old.Exists {
			err = m.write(item.path, item.old.Data)
		} else {
			err = os.Remove(item.path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return fmt.Errorf("settings recovery incomplete: %w", err)
		}
	}
	if m.Restart != nil {
		if err = m.Restart(); err != nil {
			return fmt.Errorf("settings restored but restart failed; recovery pending: %w", err)
		}
	}
	return m.clearJournal()
}

func (m Manager) clearJournal() error {
	if err := os.Remove(m.path("settings-undo.json")); err != nil {
		return err
	}
	return syncDir(filepath.Dir(m.Config))
}

// Apply updates the pair or restores the original pair. A failed rollback leaves
// its journal in place, blocks further writes and is retried by Recover.
func (m Manager) Apply(v Values) error { return m.locked(func() error { return m.apply(v, "") }) }

func (m Manager) apply(v Values, hash string) error {
	h, err := snapshot(m.Hostapd)
	if err != nil {
		return err
	}
	c, err := snapshot(m.Config)
	if err != nil {
		return err
	}
	oldHash, err := snapshot(m.path("boot-applied.sha256"))
	if err != nil {
		return err
	}
	if !h.Exists || !c.Exists {
		return fmt.Errorf("existing hotspot and application configuration required")
	}
	host, app := map[string]string{}, map[string]string{}
	if v.SSID != "" {
		host["ssid"] = v.SSID
		app["hotspot_ssid"] = strconv.Quote(v.SSID)
	}
	if v.Password != "" {
		host["wpa_passphrase"] = v.Password
		app["hotspot_password"] = strconv.Quote(v.Password)
	}
	if v.Country != "" {
		if len(v.Country) != 2 || v.Country[0] < 'A' || v.Country[0] > 'Z' || v.Country[1] < 'A' || v.Country[1] > 'Z' {
			return fmt.Errorf("invalid country code")
		}
		host["country_code"] = v.Country
	}
	if v.Debug != "" {
		if v.Debug != "true" && v.Debug != "false" {
			return fmt.Errorf("invalid debug value")
		}
		app["debug_logging"] = v.Debug
	}
	if v.SetupCompleted {
		app["setup_completed"] = "true"
	}
	if v.DeviceIdentity != "" {
		app["device_identity"] = strconv.Quote(v.DeviceIdentity)
	}
	nextH, nextC := Rewrite(h.Data, host, false), Rewrite(c.Data, app, true)
	cfg := config.Default()
	if err = toml.NewDecoder(bytes.NewReader(nextC)).DisallowUnknownFields().Decode(cfg); err != nil {
		return fmt.Errorf("proposed application configuration invalid")
	}
	if err = cfg.Validate(); err != nil {
		return err
	}
	// Credentials sent to hostapd must pass the same validation as TOML.
	if strings.ContainsAny(v.SSID+v.Password, "\r\n") {
		return fmt.Errorf("invalid credentials")
	}
	j, err := json.Marshal(journal{h, c, oldHash})
	if err != nil {
		return err
	}
	if err = m.write(m.path("settings-undo.json"), j); err != nil {
		return err
	}
	applyErr := m.write(m.Hostapd, nextH)
	if applyErr == nil {
		applyErr = m.write(m.Config, nextC)
	}
	if applyErr == nil && m.Restart != nil {
		applyErr = m.Restart()
	}
	// Only mark the boot input applied after all writes and the restart succeed.
	if applyErr == nil && hash != "" {
		applyErr = m.write(m.path("boot-applied.sha256"), []byte(hash))
	}
	if applyErr != nil {
		if err = m.recover(); err != nil {
			return fmt.Errorf("settings apply failed; recovery pending: %w", err)
		}
		return fmt.Errorf("settings apply failed; previous settings restored: %w", applyErr)
	}
	return m.clearJournal()
}

// ApplyBoot treats values as literal text, never shell code. An unchanged boot
// input does not undo a subsequent change made in the browser.
func (m Manager) ApplyBoot(path string) error {
	return m.locked(func() error {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			if m.Restart != nil {
				return m.Restart()
			}
			return nil
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		hash := hex.EncodeToString(sum[:])
		old, _ := os.ReadFile(m.path("boot-applied.sha256"))
		if strings.TrimSpace(string(old)) == hash {
			if m.Restart != nil {
				return m.Restart()
			}
			return nil
		}
		// Preserve browser changes made after a legacy boot input. The old
		// marker did not prove restart success, so confirm it before migration.
		legacy := m.LegacyHash
		if legacy == "" {
			legacy = "/var/lib/logfalcon/config-applied-hash"
		}
		previous, _ := os.ReadFile(legacy)
		if strings.TrimSpace(string(previous)) == hash {
			if m.Restart != nil {
				if err := m.Restart(); err != nil {
					return err
				}
			}
			return m.write(m.path("boot-applied.sha256"), []byte(hash))
		}
		var v Values
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
			if !ok {
				continue
			}
			switch key {
			case "SSID":
				v.SSID = value
			case "PASSWORD":
				v.Password = value
			case "COUNTRY_CODE":
				v.Country = value
			case "DEBUG":
				v.Debug = value
			}
		}
		return m.apply(v, hash)
	})
}

// Rewrite preserves unrelated settings and comments and appends missing keys.
func Rewrite(b []byte, values map[string]string, spaced bool) []byte {
	remaining := map[string]string{}
	for k, v := range values {
		remaining[k] = v
	}
	sep := "="
	if spaced {
		sep = " = "
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if v, ok := values[key]; ok {
			lines[i] = key + sep + v
			delete(remaining, key)
		}
	}
	keys := make([]string, 0, len(remaining))
	for k := range remaining {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+sep+remaining[k])
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// InitialiseDevice is called only with a fresh-install marker. Upgrades never
// create this marker, so an existing installation is never renamed.
func (m Manager) InitialiseDevice() error {
	return m.locked(func() error {
		marker := m.path("identity.pending")
		if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		cfg, err := config.Load(m.Config)
		if err != nil {
			return err
		}
		if cfg.DeviceIdentity == "" {
			id := make([]byte, 6)
			if _, err := rand.Read(id); err != nil {
				return err
			}
			v := Values{DeviceIdentity: strings.ToUpper(hex.EncodeToString(id))}
			if cfg.HotspotSSID == "LogFalcon" {
				v.SSID = "LogFalcon-" + v.DeviceIdentity
			}
			if err := m.apply(v, ""); err != nil {
				return err
			}
		}
		if err := os.Remove(marker); err != nil {
			return err
		}
		return syncDir(filepath.Dir(m.Config))
	})
}

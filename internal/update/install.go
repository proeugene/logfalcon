package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/proeugene/logfalcon/internal/config"
	lfsync "github.com/proeugene/logfalcon/internal/sync"
	"github.com/proeugene/logfalcon/internal/util"
)

const Inbox = "/var/lib/logfalcon-update/inbox"
const StateDir = "/var/lib/logfalcon-update/state"
const KeyDir = "/usr/share/logfalcon/update-keys"

type State struct {
	Phase, Version, Commit, Message string
	Files                           []string
}

// Installer's hooks allow crash/health/space tests on a temporary filesystem.
type Installer struct {
	Root, StateDir, RuntimeDir, Inbox, Keys, Current, Architecture string
	Restart                                                        func() error
	Healthy                                                        func(Manifest) error
	Space                                                          func(int64) error
}

func System(current string) Installer {
	return Installer{StateDir: StateDir, RuntimeDir: "/run/logfalcon", Inbox: Inbox, Keys: KeyDir, Current: current, Architecture: ABI(),
		Restart: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := exec.CommandContext(ctx, "systemctl", "daemon-reload").Run(); err != nil {
				return err
			}
			return exec.CommandContext(ctx, "systemctl", "restart", "logfalcon-web.service").Run()
		},
		Healthy: func(m Manifest) error {
			cfg, err := config.Load("/etc/logfalcon/logfalcon.toml")
			if err != nil {
				return errors.New("application configuration unavailable for health check")
			}
			client := http.Client{Timeout: 2 * time.Second}
			for range 15 {
				r, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", cfg.WebPort))
				if err == nil {
					var h struct {
						Version, Commit  string
						ApplicationReady bool `json:"application_ready"`
					}
					err = json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&h)
					_ = r.Body.Close()
					if err == nil && h.Version == m.Version && h.Commit == m.Commit && h.ApplicationReady {
						return nil
					}
				}
				time.Sleep(2 * time.Second)
			}
			return errors.New("new application health confirmation timed out")
		},
		Space: func(n int64) error {
			_, free, err := util.UsedAndFreeGB("/opt/logfalcon")
			if err != nil {
				return err
			}
			if free*1024*1024*1024 < float64(n+(200<<20)) {
				return errors.New("insufficient update space")
			}
			return nil
		},
	}
}
func (i Installer) dest(path string) string {
	if i.Root == "" {
		return path
	}
	return filepath.Join(i.Root, path)
}
func (i Installer) save(s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return replace(filepath.Join(i.StateDir, "status.json"), b, 0644)
}
func ReadState(dir string) State {
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if errors.Is(err, os.ErrNotExist) {
		return State{Phase: "idle"}
	}
	var s State
	if err != nil || json.Unmarshal(b, &s) != nil {
		return State{Phase: "failed", Message: "Update state unreadable; inspect diagnostics."}
	}
	switch s.Phase {
	case "idle", "validating", "activating", "restarting", "success", "rolled-back", "failed":
	default:
		return State{Phase: "failed", Message: "Update state unreadable; inspect diagnostics."}
	}
	return s
}

// CheckConfiguration rejects incomplete upgrades rather than applying launch
// defaults to missing destructive settings. Existing bytes are never rewritten.
func CheckConfiguration(app, host string) error {
	b, err := os.ReadFile(app)
	if err != nil {
		return err
	}
	var fields map[string]any
	if err = toml.Unmarshal(b, &fields); err != nil {
		return errors.New("existing application configuration is malformed")
	}
	for _, key := range []string{"erase_after_sync", "storage_pressure_cleanup", "flash_read_compression", "hotspot_ssid", "hotspot_password", "storage_path"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("incomplete upgrade configuration: %s must be explicit", key)
		}
	}
	cfg, err := config.Load(app)
	if err != nil {
		return errors.New("existing application configuration failed validation; repair it before upgrading")
	}
	if cfg.StatusPath != config.Default().StatusPath {
		return errors.New("custom status_path requires manual integration migration; no update applied")
	}
	b, err = os.ReadFile(host)
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = v
		}
	}
	if values["ssid"] != cfg.HotspotSSID || values["wpa_passphrase"] != cfg.HotspotPassword || len(values["country_code"]) != 2 {
		return errors.New("hotspot and application settings incomplete or inconsistent")
	}
	return nil
}
func (i Installer) restore(s State) error {
	if len(s.Files) == 0 {
		return errors.New("rollback journal has no files")
	}
	// A prepared journal lists every file before activation starts. A missing
	// backup marker indicates a formerly absent file.
	for n, path := range s.Files {
		backup := filepath.Join(i.StateDir, fmt.Sprintf("previous-%d", n))
		b, err := os.ReadFile(backup)
		if errors.Is(err, os.ErrNotExist) {
			if _, e := os.Stat(backup + ".absent"); e != nil {
				return errors.New("rollback backup missing")
			}
			err = os.Remove(i.dest(path))
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else if err == nil {
			err = replace(i.dest(path), b, mode(path))
		}
		if err != nil {
			return err
		}
	}
	if i.Restart != nil {
		if err := i.Restart(); err != nil {
			return err
		}
	}
	s.Phase = "rolled-back"
	s.Message = "Previous release restored. Update failed or was interrupted."
	return i.save(s)
}
func mode(path string) os.FileMode {
	if strings.HasSuffix(path, ".sh") || (path == "/opt/logfalcon/logfalcon" || path == "/opt/logfalcon/updater") {
		return 0755
	}
	return 0644
}
func replace(path string, b []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// AtomicWrite creates 0600 files; chmod the temporary file before publication.
	f, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()); _ = f.Close() }()
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
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
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
func (i Installer) Run() error {
	if err := os.MkdirAll(i.StateDir, 0755); err != nil {
		return err
	}
	lock, err := lfsync.AcquireTransferLock(i.RuntimeDir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	lockInfo, err := lock.Stat()
	if err != nil {
		return err
	}
	stat, ok := lockInfo.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return errors.New("shared lock must have exactly one link")
	}
	// The root worker may be the first process to create this shared inode.
	// Preserve it and give it back to the runtime directory's service account;
	// replacing a lock file would allow simultaneous holders on different inodes.
	if os.Geteuid() == 0 {
		dir, err := os.Stat(i.RuntimeDir)
		if err != nil {
			return err
		}
		owner, ok := dir.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("runtime directory ownership unavailable")
		}
		if err := lock.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
			return fmt.Errorf("shared lock ownership: %w", err)
		}
	}
	if err := lock.Chmod(0644); err != nil {
		return fmt.Errorf("shared lock permissions: %w", err)
	}
	if _, err := os.Stat(filepath.Join(i.RuntimeDir, "shutdown.pending")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("shutdown pending or reservation unreadable")
	}
	previous := ReadState(i.StateDir)
	if previous.Message == "Update state unreadable; inspect diagnostics." {
		return errors.New(previous.Message)
	}
	if previous.Phase == "activating" || previous.Phase == "restarting" {
		if err = i.restore(previous); err != nil {
			return fmt.Errorf("update recovery pending: %w", err)
		}
		_ = os.Remove(filepath.Join(i.Inbox, "request"))
		return nil
	}
	request := filepath.Join(i.Inbox, "request")
	if _, err = os.Stat(request); errors.Is(err, os.ErrNotExist) {
		if previous.Phase == "validating" {
			return i.save(State{Phase: "failed", Message: "Validation interrupted before activation. Upload again."})
		}
		return nil
	} else if err != nil {
		return err
	}
	fail := func(err error) error {
		_ = os.Remove(request)
		_ = i.save(State{Phase: "failed", Message: err.Error()})
		return err
	}
	if i.Root == "" {
		osrelease, err := os.ReadFile("/etc/os-release")
		if err != nil || !strings.Contains(string(osrelease), "VERSION_CODENAME=bookworm") {
			return fail(errors.New("unsupported base system: Bookworm required"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "/opt/logfalcon/logfalcon", "--version").Output()
		cancel()
		fields := strings.Fields(string(out))
		if err != nil || len(fields) != 3 || fields[0] != "logfalcon" {
			return fail(errors.New("installed release identity unavailable"))
		}
		i.Current = fields[1]
	}

	if err = i.save(State{Phase: "validating", Message: "Checking signature and installed configuration"}); err != nil {
		return err
	}
	if err = os.Remove(request); err != nil {
		return fail(err)
	}
	// Read through an anchored directory, rejecting a web-owned symlink.
	root, err := os.OpenRoot(i.Inbox)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat("package.lfu")
	if err != nil || !info.Mode().IsRegular() {
		return fail(errors.New("update upload must be a regular file"))
	}
	f, err := root.Open("package.lfu")
	if err != nil {
		return fail(err)
	}
	defer func() { _ = f.Close() }()
	// Snapshot web-owned input before authentication. Reverification is over these
	// immutable bytes, never a file the web account can mutate after validation.
	b, err := io.ReadAll(io.LimitReader(f, MaxUpload+1))
	if err != nil {
		return fail(err)
	}
	keys, err := Keys(i.Keys)
	if err != nil {
		return fail(err)
	}
	pkg, err := Verify(strings.NewReader(string(b)), int64(len(b)), keys, i.Architecture, i.Current)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = root.Remove("package.lfu") }()
	if err = CheckConfiguration(i.dest("/etc/logfalcon/logfalcon.toml"), i.dest("/etc/logfalcon/hostapd.conf")); err != nil {
		return fail(err)
	}
	var size int64
	for _, b := range pkg.Files {
		size += int64(len(b)) * 2
	}
	if i.Space != nil {
		if err = i.Space(size); err != nil {
			return fail(err)
		}
	}
	paths := map[string][]byte{"/opt/logfalcon/updater": pkg.Files["bin/logfalcon"]}
	for n, b := range pkg.Files {
		path := Destinations[n]
		if strings.HasPrefix(n, "keys/") {
			path = KeyDir + "/" + strings.TrimPrefix(n, "keys/")
		}
		paths[path] = b
	}
	s := State{Phase: "activating", Version: pkg.Manifest.Version, Commit: pkg.Manifest.Commit, Message: "Installing verified release"}
	for p := range paths {
		s.Files = append(s.Files, p)
	}
	sort.Strings(s.Files)
	// Complete durable backups before publishing the activation journal.
	for n, p := range s.Files {
		backup := filepath.Join(i.StateDir, fmt.Sprintf("previous-%d", n))
		_ = os.Remove(backup)
		_ = os.Remove(backup + ".absent")
		info, err := os.Lstat(i.dest(p))
		if errors.Is(err, os.ErrNotExist) {
			err = replace(backup+".absent", nil, 0600)
		} else if err == nil {
			if !info.Mode().IsRegular() {
				return fail(errors.New("installed asset is not a regular file"))
			}
			old, e := os.ReadFile(i.dest(p))
			if e != nil {
				return fail(e)
			}
			err = replace(backup, old, 0600)
		}
		if err != nil {
			return fail(err)
		}
	}
	if err = i.save(s); err != nil {
		return err
	}
	rollback := func(cause error) error {
		if err := i.restore(s); err != nil {
			return fmt.Errorf("%v; rollback pending: %w", cause, err)
		}
		return cause
	}
	for _, p := range s.Files {
		if p == "/opt/logfalcon/updater" {
			continue
		}
		if err = replace(i.dest(p), paths[p], mode(p)); err != nil {
			return rollback(err)
		}
	}
	s.Phase = "restarting"
	s.Message = "Waiting for application health confirmation"
	if err = i.save(s); err != nil {
		return rollback(err)
	}
	if i.Restart != nil {
		if err = i.Restart(); err != nil {
			return rollback(err)
		}
	}
	if i.Healthy == nil {
		return rollback(errors.New("health confirmation unavailable"))
	}
	if err = i.Healthy(pkg.Manifest); err != nil {
		return rollback(err)
	}
	if err = replace(i.dest("/opt/logfalcon/updater"), pkg.Files["bin/logfalcon"], 0755); err != nil {
		return rollback(err)
	}
	s.Phase = "success"
	s.Message = "Installed release confirmed healthy"
	return i.save(s)
}

// Package update verifies complete offline release packages. No uploaded code is
// executed until the authenticated release has passed all compatibility checks.
package update

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const MaxUpload = 32 << 20
const MaxExpanded = 64 << 20
const Format = 1
const Base = "bookworm-systemd-v1"

type File struct {
	SHA256 string
	Size   int64
}
type Certificate struct{ ID, PublicKey, SignedBy, Signature string }
type Manifest struct {
	Certificates                               []Certificate
	Format                                     int
	Version, Commit, Architecture, Base, KeyID string
	Migration                                  int
	Files                                      map[string]File
}
type Package struct {
	Manifest Manifest
	Files    map[string][]byte
}

var safeID = regexp.MustCompile("^[a-zA-Z0-9_-]{1,48}$")
var commitID = regexp.MustCompile("^[a-f0-9]{40}$")

// Destinations is deliberately fixed: packages cannot supply scripts to run or
// choose installation paths. Shell integration files are release-owned assets.
var Destinations = map[string]string{
	"bin/logfalcon": "/opt/logfalcon/logfalcon",
}

func init() {
	for _, name := range []string{"logfalcon@.service", "logfalcon-web.service", "logfalcon-config-apply.service", "logfalcon-wifi-init.service", "logfalcon-ready-led.service", "logfalcon-boot-led.service", "logfalcon-runtime.service", "logfalcon-diagnostics.service", "logfalcon-diagnostics-export.service", "logfalcon-diagnostics-export.timer", "logfalcon-shutdown.service", "logfalcon-update.service", "logfalcon-update.path"} {
		Destinations["system/"+name] = "/etc/systemd/system/" + name
	}
	for name, dest := range map[string]string{"config-apply.sh": "config-apply.sh", "wifi-init.sh": "wifi-init.sh", "logfalcon-boot-led.sh": "boot-led.sh"} {
		Destinations["system/"+name] = "/opt/logfalcon/" + dest
	}
	Destinations["system/50-logfalcon.rules"] = "/etc/polkit-1/rules.d/50-logfalcon.rules"
	Destinations["system/99-betaflight-fc.rules"] = "/etc/udev/rules.d/99-betaflight-fc.rules"
	Destinations["system/journald.conf"] = "/etc/systemd/journald.conf.d/logfalcon.conf"
}
func Allowed(name string) bool {
	if _, ok := Destinations[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "keys/") && strings.HasSuffix(name, ".pub") && safeID.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "keys/"), ".pub"))
}
func ABI() string {
	if runtime.GOOS != "linux" {
		return "unsupported"
	}
	switch runtime.GOARCH {
	case "arm":
		return "arm6"
	case "arm64", "amd64":
		return runtime.GOARCH
	}
	return "unsupported"
}
func Keys(dir string) (map[string]ed25519.PublicKey, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	keys := map[string]ed25519.PublicKey{}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".pub") {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			key, err := hex.DecodeString(strings.TrimSpace(string(b)))
			if err != nil || len(key) != ed25519.PublicKeySize {
				return nil, errors.New("invalid trusted update key")
			}
			keys[strings.TrimSuffix(e.Name(), ".pub")] = ed25519.PublicKey(key)
		}
	}
	return keys, nil
}
func Verify(r io.ReaderAt, size int64, keys map[string]ed25519.PublicKey, arch, current string) (*Package, error) {
	if size <= 0 || size > MaxUpload {
		return nil, errors.New("update file exceeds upload limit")
	}
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, errors.New("invalid update archive")
	}
	if len(z.File) > 64 {
		return nil, errors.New("too many update entries")
	}
	files := map[string][]byte{}
	var total uint64
	for _, f := range z.File {
		if _, ok := files[f.Name]; ok {
			return nil, errors.New("duplicate update entry")
		}
		if f.Mode()&os.ModeType != 0 || (!Allowed(f.Name) && f.Name != "manifest.json" && f.Name != "manifest.sig") {
			return nil, errors.New("unsafe or unexpected update entry")
		}
		total += f.UncompressedSize64
		if total > MaxExpanded || f.UncompressedSize64 > MaxExpanded {
			return nil, errors.New("expanded update too large")
		}
		rd, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rd, MaxExpanded+1))
		closeErr := rd.Close()
		if err != nil || closeErr != nil || uint64(len(b)) != f.UncompressedSize64 {
			return nil, errors.New("invalid update contents")
		}
		files[f.Name] = b
	}
	raw, sig := files["manifest.json"], files["manifest.sig"]
	if len(raw) > 65536 || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("invalid signed metadata")
	}
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return nil, errors.New("malformed update manifest")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing manifest data")
	}
	trusted := map[string]ed25519.PublicKey{}
	for id, k := range keys {
		trusted[id] = k
	}
	if len(m.Certificates) > 16 {
		return nil, errors.New("too many rotation certificates")
	}
	for range len(m.Certificates) {
		for _, cert := range m.Certificates {
			pub, err := hex.DecodeString(cert.PublicKey)
			signature, e := hex.DecodeString(cert.Signature)
			if err != nil || e != nil || len(pub) != 32 || len(signature) != 64 || !safeID.MatchString(cert.ID) {
				return nil, errors.New("invalid rotation certificate")
			}
			signer := trusted[cert.SignedBy]
			if len(signer) != 32 {
				continue
			}
			if !ed25519.Verify(signer, CertificateMessage(cert.ID, cert.PublicKey), signature) {
				return nil, errors.New("rotation certificate signature invalid")
			}
			if old, ok := trusted[cert.ID]; ok && !bytes.Equal(old, pub) {
				return nil, errors.New("rotation key ID conflict")
			}
			trusted[cert.ID] = ed25519.PublicKey(pub)
		}
	}
	key := trusted[m.KeyID]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, raw, sig) {
		return nil, errors.New("update signature not trusted")
	}
	if m.Format != Format || m.Base != Base || m.Migration != 1 || m.Architecture != arch || !commitID.MatchString(m.Commit) {
		return nil, errors.New("incompatible update format, base, architecture or migration")
	}
	if !Newer(m.Version, current) {
		return nil, errors.New("unsupported version or downgrade; a newer release is required")
	}
	delete(files, "manifest.json")
	delete(files, "manifest.sig")
	if len(files) != len(m.Files) || len(files) < len(Destinations) {
		return nil, errors.New("incomplete release payload")
	}
	for name := range Destinations {
		if _, ok := files[name]; !ok {
			return nil, fmt.Errorf("missing release asset %s", name)
		}
	}
	for name, b := range files {
		desc, ok := m.Files[name]
		sum := sha256.Sum256(b)
		if !ok || desc.Size != int64(len(b)) || desc.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, errors.New("update hash mismatch")
		}
		if strings.HasPrefix(name, "keys/") {
			key, err := hex.DecodeString(strings.TrimSpace(string(b)))
			if err != nil || len(key) != 32 {
				return nil, errors.New("invalid rotation key")
			}
		}
	}
	binary := files["bin/logfalcon"]
	ef, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		return nil, errors.New("release executable is not ELF")
	}
	expected := map[string]elf.Machine{"arm6": elf.EM_ARM, "arm64": elf.EM_AARCH64, "amd64": elf.EM_X86_64}
	if ef.Machine != expected[arch] || ef.Data != elf.ELFDATA2LSB {
		return nil, errors.New("executable ABI differs from manifest")
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return nil, errors.New("release executable lacks Go build identity")
	}
	identity := false
	goos, goarch, goarm := "", "", ""
	for _, setting := range info.Settings {
		switch setting.Key {
		case "GOOS":
			goos = setting.Value
		case "GOARCH":
			goarch = setting.Value
		case "GOARM":
			goarm = setting.Value
		case "-ldflags":
			flags := strings.Fields(setting.Value)
			v, c := "", ""
			for n := 0; n+1 < len(flags); n++ {
				if flags[n] == "-X" {
					key, value, ok := strings.Cut(flags[n+1], "=")
					if ok {
						if key == "main.Version" {
							v = value
						}
						if key == "main.BuildCommit" {
							c = value
						}
					}
				}
			}
			identity = v == m.Version && c == m.Commit
		}
	}
	if goos != "linux" || (arch == "arm6" && (goarch != "arm" || !strings.HasPrefix(goarm, "6"))) || (arch != "arm6" && goarch != arch) {
		return nil, errors.New("go executable ABI differs from signed metadata")
	}

	if !identity {
		return nil, errors.New("executable identity differs from signed metadata")
	}
	return &Package{m, files}, nil
}

var versionRE = regexp.MustCompile(`^v?\.?(\d+)\.(\d+)\.(\d+)(?:-([a-zA-Z0-9.-]+))?$`)

func version(v string) ([3]int, string, bool) {
	var out [3]int
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return out, "", false
	}
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return out, "", false
		}
		out[i] = n
	}
	return out, m[4], true
}
func Newer(target, current string) bool {
	a, ap, ok := version(target)
	if !ok {
		return false
	}
	b, bp, ok := version(current)
	if !ok {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	// Within a release accept only prerelease -> final. Ambiguous prerelease
	// ordering is rejected instead of risking a downgrade.
	return ap == "" && bp != ""
}
func Build(path string, m Manifest, files map[string][]byte, key ed25519.PrivateKey) error {
	m.Files = map[string]File{}
	for name, b := range files {
		if !Allowed(name) {
			return errors.New("unexpected package file")
		}
		sum := sha256.Sum256(b)
		m.Files[name] = File{hex.EncodeToString(sum[:]), int64(len(b))}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	all := map[string][]byte{"manifest.json": raw, "manifest.sig": ed25519.Sign(key, raw)}
	for n, b := range files {
		all[n] = b
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	z := zip.NewWriter(f)
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := z.Create(n)
		if err != nil {
			return err
		}
		if _, err = w.Write(all[n]); err != nil {
			return err
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	return f.Sync()
}

// CertificateMessage separates key delegation from release metadata signatures.
func CertificateMessage(id, public string) []byte {
	return []byte("LogFalcon update key v1\n" + id + "\n" + public)
}

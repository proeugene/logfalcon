// update-package is a maintainer/CI utility, not an appliance dependency.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/proeugene/logfalcon/internal/update"
	"os"
	"path/filepath"
	"strings"
)

func run() error {
	var root, binary, out, version, commit, arch, id, verify, public, certify, signedBy string
	flag.StringVar(&root, "root", ".", "Release source checkout")
	flag.StringVar(&binary, "binary", "", "Release binary")
	flag.StringVar(&out, "out", "", "Output .lfu file")
	flag.StringVar(&version, "version", "", "Release tag")
	flag.StringVar(&commit, "commit", "", "Full release commit")
	flag.StringVar(&arch, "arch", "", "Installed ABI: arm6, arm64, amd64")
	flag.StringVar(&id, "key-id", "", "Trusted signing key ID")
	flag.StringVar(&verify, "verify", "", "Verify package instead of building")
	flag.StringVar(&public, "public-key", "", "Derive public key into this file (0600 seed is read from UPDATE_SIGNING_SEED)")
	flag.StringVar(&certify, "certify", "", "Delegate to this existing public key file")
	flag.StringVar(&signedBy, "signed-by", "", "Current trusted signing key ID for delegation")
	flag.Parse()
	if verify != "" {
		f, err := os.Open(verify)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		keys, err := update.Keys(filepath.Join(root, "update-keys"))
		if err != nil {
			return err
		}
		p, err := update.Verify(f, info.Size(), keys, arch, "v0.0.0")
		if err != nil {
			return err
		}
		fmt.Printf("Verified %s %s %s\n", p.Manifest.Version, p.Manifest.Commit, p.Manifest.Architecture)
		return nil
	}
	seed, err := hex.DecodeString(strings.TrimSpace(os.Getenv("UPDATE_SIGNING_SEED")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("UPDATE_SIGNING_SEED must be a 32-byte hex Ed25519 seed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	if certify != "" {
		b, err := os.ReadFile(certify)
		if err != nil {
			return err
		}
		pub := strings.TrimSpace(string(b))
		decoded, err := hex.DecodeString(pub)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("invalid delegated public key")
		}
		cert := update.Certificate{ID: id, PublicKey: pub, SignedBy: signedBy, Signature: hex.EncodeToString(ed25519.Sign(key, update.CertificateMessage(id, pub)))}
		b, err = json.Marshal(cert)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if public != "" {
		return os.WriteFile(public, []byte(hex.EncodeToString(key.Public().(ed25519.PublicKey))+"\n"), 0644)
	}
	keys, err := update.Keys(filepath.Join(root, "update-keys"))
	if err != nil {
		return err
	}
	if !key.Public().(ed25519.PublicKey).Equal(keys[id]) {
		return fmt.Errorf("signing key does not match provisioned public key")
	}
	files := map[string][]byte{}
	for name := range update.Destinations {
		source := filepath.Join(root, name)
		if name == "bin/logfalcon" {
			source = binary
		}
		b, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		files[name] = b
	}
	entries, err := os.ReadDir(filepath.Join(root, "update-keys"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pub") {
			b, err := os.ReadFile(filepath.Join(root, "update-keys", e.Name()))
			if err != nil {
				return err
			}
			files["keys/"+e.Name()] = b
		}
	}
	m := update.Manifest{Format: update.Format, Version: version, Commit: commit, Architecture: arch, Base: update.Base, KeyID: id, Migration: 1}
	if b, err := os.ReadFile(filepath.Join(root, "update-keys", "rotation.json")); err == nil {
		if err = json.Unmarshal(b, &m.Certificates); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = update.Build(out, m, files, key); err != nil {
		return err
	}
	f, err := os.Open(out)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = update.Verify(f, info.Size(), keys, arch, "v0.0.0")
	return err
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

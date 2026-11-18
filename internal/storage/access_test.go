package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyRejectsSymlinkedOutput(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	info := testFCInfo()
	name := "fc_" + info.Variant + "_uid-" + info.UID[:8]
	if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := MakeSessionDir(root, info); err == nil {
		t.Fatal("copy followed controller symlink")
	}
	if err := os.Remove(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	dir, err := MakeSessionDir(root, info)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "keep")
	if err = os.WriteFile(secret, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(secret, filepath.Join(dir, RawFlashFilename+".partial")); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSessionWriter(root, dir); err == nil {
		t.Fatal("partial output symlink accepted")
	}
	b, err := os.ReadFile(secret)
	if err != nil || string(b) != "keep" {
		t.Fatal("outside file changed")
	}
}

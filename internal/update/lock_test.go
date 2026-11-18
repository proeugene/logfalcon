package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerRejectsHardLinkedLock(t *testing.T) {
	dir := t.TempDir()
	runtime := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtime, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(runtime, "transfer.lock")); err != nil {
		t.Fatal(err)
	}
	i := Installer{RuntimeDir: runtime, StateDir: filepath.Join(dir, "state"), Inbox: filepath.Join(dir, "inbox")}
	if err := i.Run(); err == nil {
		t.Fatal("hard-linked lock accepted")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("target permissions changed:", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("target content changed:", err)
	}
}

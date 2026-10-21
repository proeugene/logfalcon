package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImageCachePolicy(t *testing.T) {
	for _, tt := range []struct {
		ref, head, want string
		clean           bool
	}{
		{"refs/heads/main", "", "clean", false},
		{"refs/tags/v0.5.0", "", "clean", false},
		{"refs/tags/v0.5.0-rc1", "", "clean", false},
		{"refs/heads/test", "", "cached", false},
		{"refs/heads/feature/cache", "", "cached", false},
		{"refs/heads/test", "", "clean", true},
		{"refs/pull/1/merge", "test", "cached", false},
		{"unknown", "", "clean", false},
	} {
		t.Run(tt.ref+tt.want, func(t *testing.T) {
			args := []string{"../../pi-gen/build.sh", "--cache-policy"}
			if tt.clean {
				args = append(args, "--clean")
			}
			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(), "GITHUB_REF="+tt.ref, "GITHUB_HEAD_REF="+tt.head)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != tt.want {
				t.Fatalf("policy: %v %q, want %q", err, out, tt.want)
			}
		})
	}
}

func TestLocalImageCachePolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "pi-gen"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build.sh", "cache.sh"} {
		data, err := os.ReadFile("../../pi-gen/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pi-gen", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, out)
		}
	}
	check := func(want string) {
		t.Helper()
		cmd := exec.Command("bash", "pi-gen/build.sh", "--cache-policy")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GITHUB_REF=", "GITHUB_HEAD_REF=")
		if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != want {
			t.Fatalf("local policy: %v %q, want %s", err, out, want)
		}
	}
	git("init", "-q", "-b", "test")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
	check("cached")
	git("checkout", "-q", "-b", "main")
	check("clean")
	git("tag", "v0.5.0")
	git("checkout", "-q", "--detach")
	check("clean")
	git("tag", "-d", "v0.5.0")
	check("clean")
}

func TestImageCacheSnapshotsAndFreshApplication(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GNU tar/flock: exercised in Linux integration")
	}
	helper, err := filepath.Abs("../../pi-gen/cache.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", `
source "$1"
cd "$2"
key=$(printf 'a%.0s' {1..64})
mkdir -p pristine/etc pristine/opt cache
echo original > pristine/etc/os-release
ln pristine/etc/os-release pristine/etc/hardlink
ln -s os-release pristine/etc/symlink
chmod 0640 pristine/etc/os-release
os_cache_save "$PWD/cache" "$key" "$PWD/pristine" builder-original
os_cache_restore "$PWD/cache" "$key" "$PWD/restored" > restored.info
cmp pristine/etc/os-release restored/etc/os-release
test "$(stat -c %a restored/etc/os-release)" = 640
test "$(stat -c %i restored/etc/os-release)" = "$(stat -c %i restored/etc/hardlink)"
test "$(readlink restored/etc/symlink)" = os-release
grep -q builder-original restored.info

# A cold and a warm run both run current application/export stages. A clean
# run ignores a compatible cache. The fixture stands in for slow OS packages.
make_builder() {
 mkdir -p "$1"/stage{0,1,2} "$1/stage-logfalcon"
 cat > "$1/build.sh" <<'BUILD'
for stage in stage0 stage1 stage2; do
 if [[ ! -f "$stage/SKIP" ]]; then
  echo "$stage" >> stages
  mkdir -p "$WORK_DIR/$stage/rootfs/etc"
  echo fresh > "$WORK_DIR/$stage/rootfs/etc/os-release"
 fi
done
echo application-current >> stages
test -f "$WORK_DIR/stage2/rootfs/etc/os-release"
echo export-current >> stages
BUILD
}
export PIGEN_COMMIT=fixture
make_builder "$PWD/warm"
run_pigen "$PWD/warm" "$PWD/cache" "$key" cached "$PWD/warm-info"
! grep -q stage0 warm/stages
grep -q application-current warm/stages
grep -q export-current warm/stages
grep -q 'OS cache reused: 1' warm-info
cmp pristine/etc/os-release warm/work/logfalcon/stage2/rootfs/etc/os-release
make_builder "$PWD/clean"
run_pigen "$PWD/clean" "$PWD/cache" "$key" clean "$PWD/clean-info"
grep -q stage0 clean/stages
grep -q 'OS cache reused: 0' clean-info
grep -q fresh clean/work/logfalcon/stage2/rootfs/etc/os-release

# Corrupt, incomplete and incompatible entries are misses, never partial roots.
echo corrupt >> "cache/$key/rootfs.tar"
if os_cache_restore "$PWD/cache" "$key" "$PWD/corrupt"; then exit 40; fi
test ! -e corrupt
echo not-a-tar > "cache/$key/rootfs.tar"
(cd "cache/$key" && sha256sum rootfs.tar > rootfs.sha256)
if os_cache_restore "$PWD/cache" "$key" "$PWD/broken-tar"; then exit 45; fi
test ! -e broken-tar
make_builder "$PWD/cold"
run_pigen "$PWD/cold" "$PWD/cache" "$key" cached "$PWD/cold-info"
grep -q stage0 cold/stages
grep -q application-current cold/stages
os_cache_save "$PWD/cache" "$key" "$PWD/pristine" refreshed
rm "cache/$key/info"
if os_cache_restore "$PWD/cache" "$key" "$PWD/incomplete"; then exit 41; fi
other=$(printf 'b%.0s' {1..64})
if os_cache_restore "$PWD/cache" "$other" "$PWD/missing"; then exit 42; fi

# Failed/new application stages must not replace an existing valid snapshot.
os_cache_save "$PWD/cache" "$key" "$PWD/pristine" previous-good
before=$(sha256sum "cache/$key/rootfs.tar")
make_builder "$PWD/failed"
echo 'exit 42' >> failed/build.sh
if run_pigen "$PWD/failed" "$PWD/cache" "$key" clean "$PWD/failed-info"; then exit 43; fi
test "$before" = "$(sha256sum "cache/$key/rootfs.tar")"

# Never publish installed application or per-device configuration into the base.
mkdir -p pristine/etc/logfalcon
if os_cache_save "$PWD/cache" "$key" "$PWD/pristine" bad; then exit 44; fi
test "$before" = "$(sha256sum "cache/$key/rootfs.tar")"
`, "test", helper, t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("OS cache lifecycle: %v\n%s", err, out)
	}
}

func TestImageCacheKeyTracksOSInputs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("sha256sum: exercised in Linux integration")
	}
	helper, err := filepath.Abs("../../pi-gen/cache.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", `
source "$1"
cd "$2"
mkdir -p one/pi-gen two/pi-gen one/internal
for file in config build.sh cache.sh; do echo fixture > "one/pi-gen/$file"; done
cp one/pi-gen/* two/pi-gen/
before=$(os_cache_key "$PWD/one")
test "$before" = "$(os_cache_key "$PWD/two")"
echo new-app > one/internal/application.go
test "$before" = "$(os_cache_key "$PWD/one")"
for file in config build.sh cache.sh; do
 echo changed >> "one/pi-gen/$file"
 test "$before" != "$(os_cache_key "$PWD/one")"
 cp "two/pi-gen/$file" "one/pi-gen/$file"
done
`, "test", helper, t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("OS cache invalidation: %v\n%s", err, out)
	}
}

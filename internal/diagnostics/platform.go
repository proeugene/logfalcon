package diagnostics

import (
	"fmt"
	"os"
	"syscall"
)

func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlockFile(f *os.File)     { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// BootPartition refuses an ordinary rootfs directory if the FAT card partition
// failed to mount. This avoids accidentally filling the Linux root filesystem.
func BootPartition() (string, error) {
	for _, dir := range []string{"/boot/firmware", "/boot"} {
		var stat syscall.Statfs_t
		if syscall.Statfs(dir, &stat) == nil && uint64(stat.Type) == 0x4d44 {
			return dir, nil
		}
	}
	return "", fmt.Errorf("mounted FAT boot partition not found")
}

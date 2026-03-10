package util

import "syscall"

// FreeBytes returns available bytes at path using syscall.Statfs.
func FreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// FreeMB returns available megabytes at path.
func FreeMB(path string) (float64, error) {
	b, err := FreeBytes(path)
	if err != nil {
		return 0, err
	}
	return float64(b) / (1024 * 1024), nil
}

// UsedAndFreeGB returns (usedGB, freeGB) for the filesystem containing path.
func UsedAndFreeGB(path string) (float64, float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bavail) * int64(stat.Bsize)
	used := total - free
	const gb = 1024 * 1024 * 1024
	return float64(used) / gb, float64(free) / gb, nil
}

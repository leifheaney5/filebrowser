//go:build !windows

package organizer

import (
	"golang.org/x/sys/unix"
	"os"
)

func syncDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func availableSpace(dir string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(dir, &stat)
	return uint64(stat.Bavail) * uint64(stat.Bsize), err
}

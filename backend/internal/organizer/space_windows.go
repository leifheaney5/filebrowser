package organizer

import (
	"golang.org/x/sys/windows"
	"os"
)

// Windows does not expose directory fsync through os.File. Production Linux
// additionally syncs destination directory entries before removing originals.
func syncDirectory(_ *os.Root, _ string) error { return nil }

func availableSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	err = windows.GetDiskFreeSpaceEx(p, &available, &total, &free)
	return available, err
}

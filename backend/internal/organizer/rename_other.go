//go:build !linux

package organizer

import "os"

const renameSupported = false

func renameNoReplace(_ *os.Root, _ string, _ *os.Root, _ string, _ os.FileInfo) (bool, error) {
	return false, nil
}

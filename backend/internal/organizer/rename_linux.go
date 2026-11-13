package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const renameSupported = true

// renameNoReplace anchors both operands to already opened directory handles.
// RENAME_NOREPLACE makes conflict protection atomic with the rename itself.
// A kernel/filesystem without the required primitive falls back to exclusive
// copying; permission and conflict errors are never treated as fallback hints.
func renameNoReplace(src *os.Root, source string, dst *os.Root, target string, original os.FileInfo) (bool, error) {
	openParent := func(root *os.Root, name string) (*os.File, error) {
		parent := filepath.Dir(name)
		if err := CheckAncestors(root, parent); err != nil {
			return nil, err
		}
		before, err := root.Lstat(parent)
		if err != nil {
			return nil, err
		}
		f, err := root.Open(parent)
		if err != nil {
			return nil, err
		}
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) {
			f.Close()
			return nil, fmt.Errorf("parent directory changed while opening")
		}
		return f, nil
	}
	sourceParent, err := openParent(src, source)
	if err != nil {
		return false, err
	}
	defer sourceParent.Close()
	targetParent, err := openParent(dst, target)
	if err != nil {
		return false, err
	}
	defer targetParent.Close()
	if err = CheckAncestors(src, source); err != nil {
		return false, err
	}
	if !same(src, entry{source, original}) {
		return false, fmt.Errorf("source changed before rename")
	}
	// Recheck each logical parent still names the descriptor we opened. The
	// syscall then uses descriptors even if an ancestor changes afterwards.
	for _, parent := range []struct {
		root *os.Root
		name string
		file *os.File
	}{{src, filepath.Dir(source), sourceParent}, {dst, filepath.Dir(target), targetParent}} {
		current, e := parent.root.Lstat(parent.name)
		if e != nil {
			return false, e
		}
		opened, e := parent.file.Stat()
		if e != nil {
			return false, e
		}
		if current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, opened) {
			return false, fmt.Errorf("parent directory changed before rename")
		}
	}
	err = unix.Renameat2(int(sourceParent.Fd()), filepath.Base(source), int(targetParent.Fd()), filepath.Base(target), unix.RENAME_NOREPLACE)
	if err == nil {
		// Sync the exact directory handles used by rename, even if another
		// process changes their logical paths between rename and fsync.
		if err = targetParent.Sync(); err != nil {
			return true, err
		}
		if err = sourceParent.Sync(); err != nil {
			return true, err
		}
		return true, nil
	}
	if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return false, nil
	}
	return false, &os.LinkError{Op: "rename", Old: source, New: target, Err: err}
}

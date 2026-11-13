package organizer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Location confines every filesystem call to an opened source scope. Check is
// invoked for every descendant, including immediately before destructive work.
type Location struct {
	Root     string
	Relative string
	Check    func(string) error
	Open     func() (*os.Root, error)
	Verify   func() error
}

func openLocation(l Location) (*os.Root, error) {
	if l.Open != nil {
		return l.Open()
	}
	return os.OpenRoot(l.Root)
}

type entry struct {
	name string
	info os.FileInfo
}

// CleanRelative rejects ambiguous Windows paths even on a Linux host.
func CleanRelative(name string) (string, error) {
	if strings.ContainsAny(name, "\\\x00:") {
		return "", fmt.Errorf("invalid path")
	}
	for _, s := range strings.Split(name, "/") {
		if s == ".." {
			return "", fmt.Errorf("parent traversal is not allowed")
		}
	}
	out := filepath.Clean(strings.TrimPrefix(name, "/"))
	if !filepath.IsLocal(out) {
		return "", fmt.Errorf("path must stay inside source scope")
	}
	return out, nil
}
func check(l Location, name string) error {
	if l.Check != nil {
		if err := l.Check(filepath.ToSlash(name)); err != nil {
			return err
		}
	}
	return nil
}

// CheckAncestors refuses symlinks rather than following them. os.Root also
// enforces confinement if a path is replaced while the operation is running.
func CheckAncestors(root *os.Root, name string) error {
	clean, err := CleanRelative(filepath.ToSlash(name))
	if err != nil {
		return err
	}
	curr := "."
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		curr = filepath.Join(curr, part)
		st, e := root.Lstat(curr)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported by organizer")
		}
	}
	return nil
}
func inventory(r *os.Root, l Location, name string, depth int, out *[]entry) error {
	if depth > 128 || len(*out) >= 100000 {
		return fmt.Errorf("operation exceeds directory safety limit")
	}
	if err := check(l, name); err != nil {
		return err
	}
	if err := CheckAncestors(r, name); err != nil {
		return err
	}
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("only regular files and directories are supported")
	}
	*out = append(*out, entry{name, info})
	if !info.IsDir() {
		return nil
	}
	f, err := r.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		batch, readErr := f.ReadDir(128)
		for _, child := range batch {
			if err = inventory(r, l, filepath.Join(name, child.Name()), depth+1, out); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}
func same(r *os.Root, e entry) bool {
	now, err := r.Lstat(e.name)
	return err == nil && os.SameFile(now, e.info) && now.Mode() == e.info.Mode() && now.Size() == e.info.Size() && now.ModTime().Equal(e.info.ModTime())
}

// Transfer uses an atomic no-replace rename for Linux moves on one filesystem.
// Cross-device moves copy exclusively, sync data, then remove the unchanged
// original. Both paths protect destination conflicts without overwriting.
// Once a destination has been created, any error requires explicit review;
// retry never silently repeats partial filesystem work.
func Transfer(src, dst Location, remove bool) (err error) {
	src.Relative, err = CleanRelative(filepath.ToSlash(src.Relative))
	if err != nil {
		return err
	}
	dst.Relative, err = CleanRelative(filepath.ToSlash(dst.Relative))
	if err != nil {
		return err
	}
	if src.Relative == "." || dst.Relative == "." {
		return fmt.Errorf("cannot operate on source root")
	}
	srcAbs := filepath.Join(src.Root, src.Relative)
	dstAbs := filepath.Join(dst.Root, dst.Relative)
	rel, e := filepath.Rel(srcAbs, dstAbs)
	if e == nil && (rel == "." || filepath.IsLocal(rel)) {
		return fmt.Errorf("destination must be outside selected item")
	}
	sr, e := openLocation(src)
	if e != nil {
		return e
	}
	defer sr.Close()
	dr, e := openLocation(dst)
	if e != nil {
		return e
	}
	defer dr.Close()
	if e = CheckAncestors(dr, filepath.Dir(dst.Relative)); e != nil {
		return e
	}
	if e = check(dst, filepath.Dir(dst.Relative)); e != nil {
		return e
	}
	if _, e = dr.Lstat(dst.Relative); e == nil {
		return fmt.Errorf("destination already exists: %w", os.ErrExist)
	} else if !os.IsNotExist(e) {
		return e
	}
	entries := []entry{}
	if src.Verify != nil {
		if e = src.Verify(); e != nil {
			return e
		}
	}
	if e = inventory(sr, src, src.Relative, 0, &entries); e != nil {
		return e
	}
	// Source names can alias the same mount. Compare directory identities as
	// well as strings so a copy cannot create its destination inside itself.
	if entries[0].info.IsDir() {
		for parent := filepath.Dir(dst.Relative); ; parent = filepath.Dir(parent) {
			st, err := dr.Stat(parent)
			if err != nil {
				return err
			}
			if os.SameFile(st, entries[0].info) {
				return fmt.Errorf("destination must be outside selected directory")
			}
			if parent == "." {
				break
			}
		}
	}
	// Preflight all destination rules before creating anything.
	for _, it := range entries {
		suffix, _ := filepath.Rel(src.Relative, it.name)
		target := filepath.Join(dst.Relative, suffix)
		if e = check(dst, target); e != nil {
			return e
		}
	}
	mutated := false
	defer func() {
		if err != nil && mutated {
			err = fmt.Errorf("%w: %v", ErrReview, err)
		}
	}()
	if remove && renameSupported {
		// Revalidate the complete inventory immediately before a directory rename
		// can move it as a unit. Duplicate cleanup also verifies its retained copy.
		if src.Verify != nil {
			if e = src.Verify(); e != nil {
				return e
			}
		}
		for _, it := range entries {
			if e = check(src, it.name); e != nil {
				return e
			}
			suffix, _ := filepath.Rel(src.Relative, it.name)
			if e = check(dst, filepath.Join(dst.Relative, suffix)); e != nil {
				return e
			}
			if e = CheckAncestors(sr, it.name); e != nil {
				return e
			}
			if !same(sr, it) {
				return fmt.Errorf("source changed before move")
			}
		}
		renamed, renameErr := renameNoReplace(sr, src.Relative, dr, dst.Relative, entries[0].info)
		mutated = renamed
		if renameErr != nil {
			return renameErr
		}
		if renamed {
			return nil
		}
	}
	// Capacity is needed only for a new physical copy. Same-device renames
	// remain usable without requiring enough free space for a second library.
	var required uint64
	for _, it := range entries {
		if it.info.Mode().IsRegular() {
			required += uint64(it.info.Size())
		}
	}
	available, e := availableSpace(dst.Root)
	if e != nil {
		return fmt.Errorf("cannot check destination capacity: %w", e)
	}
	const reserve = 256 << 20
	if available < reserve || required > available-reserve {
		return fmt.Errorf("insufficient destination space; 256 MiB reserve required")
	}
	for _, it := range entries {
		suffix, _ := filepath.Rel(src.Relative, it.name)
		target := filepath.Join(dst.Relative, suffix)
		if e = check(src, it.name); e != nil {
			return e
		}
		if e = check(dst, target); e != nil {
			return e
		}
		if e = CheckAncestors(sr, it.name); e != nil {
			return e
		}
		if !same(sr, it) {
			return fmt.Errorf("source changed during operation")
		}
		if e = CheckAncestors(dr, filepath.Dir(target)); e != nil {
			return e
		}
		if it.info.IsDir() {
			if e = dr.Mkdir(target, it.info.Mode().Perm()); e != nil {
				return e
			}
			mutated = true
			continue
		}
		input, e := sr.Open(it.name)
		if e != nil {
			return e
		}
		st, e := input.Stat()
		if e != nil || !os.SameFile(st, it.info) {
			input.Close()
			return fmt.Errorf("source changed during operation")
		}
		output, e := dr.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, it.info.Mode().Perm())
		if e != nil {
			input.Close()
			return e
		}
		mutated = true
		_, e = io.CopyBuffer(output, input, make([]byte, 256*1024))
		if e == nil {
			e = output.Sync()
		}
		ce := output.Close()
		input.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if e = dr.Chtimes(target, it.info.ModTime(), it.info.ModTime()); e != nil {
			return e
		}
		if !same(sr, it) {
			return fmt.Errorf("source changed during copy")
		}
	}
	// Persist all new directory entries before a move can remove its source.
	for n := len(entries) - 1; n >= 0; n-- {
		if entries[n].info.IsDir() {
			suffix, _ := filepath.Rel(src.Relative, entries[n].name)
			if e = syncDirectory(dr, filepath.Join(dst.Relative, suffix)); e != nil {
				return e
			}
		}
	}
	if e = syncDirectory(dr, filepath.Dir(dst.Relative)); e != nil {
		return e
	}
	if !remove {
		return nil
	}
	if src.Verify != nil {
		if e = src.Verify(); e != nil {
			return e
		}
	}
	// Validate the entire source before the first deletion. Each directory must
	// still contain exactly the inventoried children (new files are never removed).
	for _, it := range entries {
		if e = check(src, it.name); e != nil {
			return e
		}
		if e = CheckAncestors(sr, it.name); e != nil {
			return e
		}
		if !same(sr, it) {
			return fmt.Errorf("source changed before removal")
		}
	}
	for n := len(entries) - 1; n >= 0; n-- {
		it := entries[n]
		if e = check(src, it.name); e != nil {
			return e
		}
		if e = CheckAncestors(sr, it.name); e != nil {
			return e
		}
		if !it.info.IsDir() && !same(sr, it) {
			return fmt.Errorf("source changed before removal")
		}
		if e = sr.Remove(it.name); e != nil {
			return e
		}
	}
	return nil
}

package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxMovePreservesFileAndDirectoryInodes(t *testing.T) {
	for _, name := range []string{"file", "directory"} {
		t.Run(name, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			child := name
			if name == "directory" {
				child = "directory/child"
			}
			write(t, src, child, "payload")
			before, err := os.Stat(filepath.Join(src, name))
			if err != nil {
				t.Fatal(err)
			}
			childBefore, err := os.Stat(filepath.Join(src, child))
			if err != nil {
				t.Fatal(err)
			}
			if err = Transfer(Location{Root: src, Relative: name}, Location{Root: dst, Relative: name}, true); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(filepath.Join(dst, name))
			if err != nil {
				t.Fatal(err)
			}
			childAfter, err := os.Stat(filepath.Join(dst, child))
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !os.SameFile(childBefore, childAfter) {
				t.Fatal("same-filesystem move copied data instead of preserving inode")
			}
			if _, err = os.Stat(filepath.Join(src, name)); !os.IsNotExist(err) {
				t.Fatal("original path remained")
			}
		})
	}
}

func TestLinuxAtomicMoveNeverOverwritesConflict(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, "file", "original")
	write(t, dst, "file", "keep")
	sr, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	dr, err := os.OpenRoot(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer dr.Close()
	before, err := sr.Stat("file")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := renameNoReplace(sr, "file", dr, "file", before)
	if renamed || !errors.Is(err, os.ErrExist) {
		t.Fatalf("conflict overwritten or fallback attempted: renamed=%v err=%v", renamed, err)
	}
	for _, test := range []struct{ root, want string }{{src, "original"}, {dst, "keep"}} {
		data, err := os.ReadFile(filepath.Join(test.root, "file"))
		if err != nil || string(data) != test.want {
			t.Fatalf("conflict changed files: %s %v", data, err)
		}
	}
}

func TestLinuxAtomicMoveRevalidatesSourceAndDuplicateGuard(t *testing.T) {
	for _, mode := range []string{"source-changed", "keeper-mismatch", "permission-revoked"} {
		t.Run(mode, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			write(t, src, "file", "original")
			calls := 0
			source := Location{Root: src, Relative: "file"}
			source.Verify = func() error {
				calls++
				if calls == 2 {
					switch mode {
					case "source-changed":
						write(t, src, "file", "different size")
					case "keeper-mismatch":
						return errors.New("keeper content mismatch")
					}
				}
				return nil
			}
			// Capture the permission callback through a separate mutable flag: Location
			// is passed by value, while live authorization is re-evaluated by closure.
			revoked := false
			source.Check = func(string) error {
				if revoked {
					return os.ErrPermission
				}
				return nil
			}
			if mode == "permission-revoked" {
				source.Verify = func() error {
					calls++
					if calls == 2 {
						revoked = true
					}
					return nil
				}
			}
			err := Transfer(source, Location{Root: dst, Relative: "file"}, true)
			if err == nil || errors.Is(err, ErrReview) {
				t.Fatalf("unsafe move or unexpected mutation: %v", err)
			}
			if _, err = os.Stat(filepath.Join(src, "file")); err != nil {
				t.Fatal("source removed after failed validation")
			}
			if _, err = os.Stat(filepath.Join(dst, "file")); !os.IsNotExist(err) {
				t.Fatal("destination created after failed validation")
			}
		})
	}
}

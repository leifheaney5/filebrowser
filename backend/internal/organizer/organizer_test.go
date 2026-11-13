package organizer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func waitJob(t *testing.T, m *Manager, owner uint64, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, e := m.Get(owner, id)
		if e != nil {
			t.Fatal(e)
		}
		if j.Status != "queued" && j.Status != "running" {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func TestDurableJobsRecoveryNeverReplaysRunning(t *testing.T) {
	dir := t.TempDir()
	state := diskState{Jobs: []*Job{{ID: "interrupted", Owner: 4, Kind: "move", Items: []Item{{Source: "s", Path: "a", Status: "running"}, {Source: "s", Path: "b", Status: "pending"}}, Total: 2}}, Trash: map[string]*Trash{}}
	data, _ := json.Marshal(state)
	if e := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	m, e := Open(dir, func(owner uint64, kind string, it Item, d Destination, tr *Trash, p string) error {
		if it.Path != "b" {
			t.Errorf("replayed running item %s", it.Path)
		}
		calls.Add(1)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	j := waitJob(t, m, 4, "interrupted")
	if j.Status != "review" || j.Items[0].Status != "review" || j.Completed != 1 || calls.Load() != 1 {
		t.Fatalf("unexpected recovered state %+v calls %d", j, calls.Load())
	}
	if _, e = m.Get(5, "interrupted"); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-user read allowed")
	}
	if _, e = m.Change(4, "interrupted", "retry"); e != nil {
		t.Fatal(e)
	}
	m.Close()
	if calls.Load() != 1 {
		t.Fatal("review retried")
	}
	m, e = Open(dir, func(uint64, string, Item, Destination, *Trash, string) error {
		t.Error("finished job replayed")
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	j, _ = m.Get(4, "interrupted")
	if j.Items[0].Status != "review" || j.Completed != 1 {
		t.Fatal("state not durable")
	}
}
func TestCancelAndRetryOnlyFailures(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	m, e := Open(t.TempDir(), func(_ uint64, _ string, it Item, _ Destination, _ *Trash, _ string) error {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			<-release
			return errors.New("permission changed")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	j, e := m.Submit(1, Request{Kind: "copy", Items: []Item{{Source: "s", Path: "a"}, {Source: "s", Path: "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	<-started
	if _, e = m.Change(2, j.ID, "cancel"); !errors.Is(e, ErrNotFound) {
		t.Fatal("other user cancelled")
	}
	if _, e = m.Change(1, j.ID, "cancel"); e != nil {
		t.Fatal(e)
	}
	close(release)
	j = waitJob(t, m, 1, j.ID)
	if j.Status != "cancelled" || calls.Load() != 1 || j.Items[1].Status != "cancelled" {
		t.Fatalf("cancel failed %+v", j)
	}
	if _, e = m.Change(1, j.ID, "retry"); e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, m, 1, j.ID)
	if calls.Load() != 2 || j.Items[0].Status != "completed" || j.Items[1].Status != "cancelled" {
		t.Fatalf("retry failed %+v", j)
	}
}
func TestTrashRestoreOwnership(t *testing.T) {
	m, e := Open(t.TempDir(), func(_ uint64, k string, it Item, _ Destination, tr *Trash, p string) error {
		if k == "trash" && tr == nil {
			t.Fatal("missing durable trash metadata")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	j, e := m.Submit(2, Request{Kind: "trash", Items: []Item{{Source: "s", Path: "/docs/a"}}})
	if e != nil {
		t.Fatal(e)
	}
	waitJob(t, m, 2, j.ID)
	trash := m.Trash(2)
	if len(trash) != 1 || len(m.Trash(3)) != 0 {
		t.Fatal("trash isolation")
	}
	if _, e = m.Submit(3, Request{Kind: "restore", Items: []Item{{TrashID: trash[0].ID}}}); e == nil {
		t.Fatal("restored another user's trash")
	}
	j, e = m.Submit(2, Request{Kind: "restore", Items: []Item{{TrashID: trash[0].ID, Source: "attacker", Path: "/evil"}}})
	if e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, m, 2, j.ID)
	if j.Items[0].Source != "s" || j.Items[0].Path != "/docs/a" || len(m.Trash(2)) != 0 {
		t.Fatal("restore did not use authoritative metadata")
	}
}
func write(t *testing.T, root, name, content string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestTransferNestedUnicodeSpacesNoOverwrite(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, "folder/音 file.txt", "hello")
	write(t, src, "folder/nested/a", "more")
	if e := Transfer(Location{Root: src, Relative: "folder"}, Location{Root: dst, Relative: "copy"}, false); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(dst, "copy/音 file.txt"))
	if e != nil || string(data) != "hello" {
		t.Fatal(e)
	}
	if e = Transfer(Location{Root: src, Relative: "folder"}, Location{Root: dst, Relative: "copy"}, true); !errors.Is(e, os.ErrExist) {
		t.Fatalf("conflict not rejected: %v", e)
	}
	if e = Transfer(Location{Root: src, Relative: "folder"}, Location{Root: dst, Relative: "moved"}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(src, "folder")); !os.IsNotExist(e) {
		t.Fatal("source remained")
	}
}
func TestTraversalAndPermissionChanges(t *testing.T) {
	for _, p := range []string{"../x", "/a/../x", "a\\..\\b", "C:/x", "a\x00b"} {
		if _, e := CleanRelative(p); e == nil {
			t.Errorf("accepted %q", p)
		}
	}
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, "a", "source")
	denied := Location{Root: src, Relative: "a", Check: func(string) error { return os.ErrPermission }}
	if e := Transfer(denied, Location{Root: dst, Relative: "b"}, true); !errors.Is(e, os.ErrPermission) {
		t.Fatalf("permission ignored %v", e)
	}
	write(t, src, "dir/blocked", "secret")
	denied = Location{Root: src, Relative: "dir", Check: func(p string) error {
		if p == "dir/blocked" {
			return os.ErrPermission
		}
		return nil
	}}
	if e := Transfer(denied, Location{Root: dst, Relative: "dir"}, true); !errors.Is(e, os.ErrPermission) {
		t.Fatalf("descendant permission ignored %v", e)
	}
	if _, e := os.Stat(filepath.Join(dst, "dir")); !os.IsNotExist(e) {
		t.Fatal("preflight changed destination")
	}
	changing := Location{Root: src, Relative: "dir", Check: func(string) error {
		if _, err := os.Stat(filepath.Join(dst, "changed")); err == nil {
			return os.ErrPermission
		}
		return nil
	}}
	if e := Transfer(changing, Location{Root: dst, Relative: "changed"}, false); !errors.Is(e, ErrReview) {
		t.Fatalf("late permission change should require review %v", e)
	}
	if _, e := os.Stat(filepath.Join(src, "dir/blocked")); e != nil {
		t.Fatal("revoked source deleted")
	}
}
func TestSymlinkAndSelfCopyRejected(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, src, "dir/a", "hello")
	if e := Transfer(Location{Root: src, Relative: "dir"}, Location{Root: src, Relative: "dir/sub"}, false); e == nil {
		t.Fatal("self copy accepted")
	}
	if e := os.Symlink(filepath.Join(src, "dir"), filepath.Join(src, "link")); e != nil {
		t.Skipf("symlinks unavailable: %v", e)
	}
	if e := Transfer(Location{Root: src, Relative: "link/a"}, Location{Root: dst, Relative: "a"}, false); e == nil {
		t.Fatal("symlink ancestor accepted")
	}
	if e := Transfer(Location{Root: src, Relative: "link"}, Location{Root: dst, Relative: "link"}, false); e == nil {
		t.Fatal("symlink accepted")
	}
}

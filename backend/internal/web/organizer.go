package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/files"
	"github.com/gtsteffaniak/filebrowser/backend/internal/organizer"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
)

func organizerBinding(location organizer.Location) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(location.Root))))
}

func organizerChecksum(location organizer.Location) (string, os.FileInfo, error) {
	if err := location.Check(location.Relative); err != nil {
		return "", nil, err
	}
	root, err := location.Open()
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	if err = organizer.CheckAncestors(root, location.Relative); err != nil {
		return "", nil, err
	}
	f, err := root.Open(location.Relative)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	if !before.Mode().IsRegular() {
		return "", nil, fmt.Errorf("duplicate cleanup requires regular files")
	}
	hash := sha256.New()
	if _, err = io.CopyBuffer(hash, f, make([]byte, 256*1024)); err != nil {
		return "", nil, err
	}
	after, err := root.Lstat(location.Relative)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", nil, fmt.Errorf("duplicate candidate changed during verification")
	}
	return hex.EncodeToString(hash.Sum(nil)), before, nil
}

func verifyOrganizerDuplicate(source, keeper organizer.Location, expected string) error {
	a, as, err := organizerChecksum(source)
	if err != nil {
		return err
	}
	b, bs, err := organizerChecksum(keeper)
	if err != nil {
		return err
	}
	if os.SameFile(as, bs) {
		return fmt.Errorf("hard links are the same file, not duplicate copies")
	}
	if a != expected || b != expected {
		return fmt.Errorf("duplicate contents changed or keeper is unavailable")
	}
	return nil
}

func organizerKeeperOutsideSelection(keeper, selected organizer.Location) error {
	selectedAbs := filepath.Join(selected.Root, selected.Relative)
	keeperAbs := filepath.Join(keeper.Root, keeper.Relative)
	relative, err := filepath.Rel(selectedAbs, keeperAbs)
	if err == nil && (relative == "." || filepath.IsLocal(relative)) {
		return fmt.Errorf("keeper must remain outside the selected items")
	}
	// Source labels may alias a mount. Inode checks also reject a selected
	// keeper reached through another configured source name or hard link.
	sr, err := selected.Open()
	if err != nil {
		return err
	}
	defer sr.Close()
	selectedStat, err := sr.Lstat(selected.Relative)
	if err != nil {
		return err
	}
	kr, err := keeper.Open()
	if err != nil {
		return err
	}
	defer kr.Close()
	for current := keeper.Relative; ; current = filepath.Dir(current) {
		stat, err := kr.Lstat(current)
		if err != nil {
			return err
		}
		if os.SameFile(selectedStat, stat) {
			return fmt.Errorf("keeper must remain outside the selected items")
		}
		if current == "." {
			break
		}
	}
	return nil
}

var organizerService struct {
	sync.Mutex
	manager *organizer.Manager
}

// InitOrganizer must run after users, sources, and indexes are initialized. It
// resumes queued items; interrupted running items are retained for review.
func InitOrganizer() error {
	organizerService.Lock()
	defer organizerService.Unlock()
	if organizerService.manager != nil {
		return nil
	}
	db := settings.Config.Server.DatabaseV2.Path
	if db == "" {
		return fmt.Errorf("organizer requires a persistent database path")
	}
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(db), "organizer"))
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	// Trash must never be reachable through any exposed source, even for an admin.
	for _, source := range settings.Config.Server.Sources {
		root, e := filepath.Abs(source.Path)
		if e != nil {
			return e
		}
		if resolved, e := filepath.EvalSymlinks(root); e == nil {
			root = resolved
		}
		rel, e := filepath.Rel(root, dir)
		if e == nil && (rel == "." || filepath.IsLocal(rel)) {
			return fmt.Errorf("organizer data directory must be outside all file sources")
		}
	}
	m, err := organizer.Open(dir, executeOrganizerItem)
	if err != nil {
		return err
	}
	organizerService.manager = m
	return nil
}

func organizerLocation(owner uint64, sourceName, clientPath, permission string) (organizer.Location, error) {
	user, err := state.GetUserByID(owner)
	if err != nil {
		return organizer.Location{}, fmt.Errorf("owner unavailable")
	}
	idx := indexing.GetIndex(sourceName)
	if idx == nil {
		return organizer.Location{}, fmt.Errorf("source unavailable")
	}
	scope, err := user.GetScopeForSourceName(sourceName)
	if err != nil {
		return organizer.Location{}, fmt.Errorf("source unavailable")
	}
	scope = filepath.ToSlash(scope)
	clean, err := organizer.CleanRelative(clientPath)
	if err != nil {
		return organizer.Location{}, err
	}
	scopeClean, err := organizer.CleanRelative(filepath.ToSlash(scope))
	if err != nil {
		return organizer.Location{}, err
	}
	// A scoped root is itself checked from the actual source root, so a symlink
	// cannot turn a user scope into a different filesystem subtree.
	sourceRoot, err := os.OpenRoot(idx.Path)
	if err != nil {
		return organizer.Location{}, err
	}
	defer sourceRoot.Close()
	if err = organizer.CheckAncestors(sourceRoot, scopeClean); err != nil {
		return organizer.Location{}, err
	}
	root := filepath.Join(idx.Path, scopeClean)
	location := organizer.Location{Root: root, Relative: clean}
	location.Open = func() (*os.Root, error) {
		base, err := os.OpenRoot(idx.Path)
		if err != nil {
			return nil, err
		}
		defer base.Close()
		if err = organizer.CheckAncestors(base, scopeClean); err != nil {
			return nil, err
		}
		before, err := base.Lstat(scopeClean)
		if err != nil {
			return nil, err
		}
		opened, err := base.OpenRoot(scopeClean)
		if err != nil {
			return nil, err
		}
		after, err := opened.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			opened.Close()
			return nil, fmt.Errorf("source scope changed while opening")
		}
		return opened, nil
	}
	location.Check = func(relative string) error {
		current, e := state.GetUserByID(owner)
		if e != nil {
			return fmt.Errorf("owner unavailable")
		}
		currentScope, e := current.GetScopeForSourceName(sourceName)
		if e != nil || utils.JoinScopedIndexPath(filepath.ToSlash(currentScope), "/") != utils.JoinScopedIndexPath(scope, "/") {
			return fmt.Errorf("source scope changed")
		}
		live := indexing.GetIndex(sourceName)
		if live == nil || live.Path != idx.Path {
			return fmt.Errorf("source changed")
		}
		perms, e := current.FilePermsForSourceName(sourceName)
		if e != nil || !perms.View {
			return fmt.Errorf("source permission denied")
		}
		allowed := false
		switch permission {
		case "read":
			allowed = perms.Download
		case "move":
			allowed = perms.Modify
		case "trash":
			allowed = perms.Delete
		case "create":
			allowed = perms.Create
		}
		if !allowed || (permission != "read" && live.Config.ReadOnly) {
			return fmt.Errorf("operation permission denied")
		}
		rel, e := organizer.CleanRelative(filepath.ToSlash(relative))
		if e != nil {
			return e
		}
		full := utils.JoinScopedIndexPath(scope, filepath.ToSlash(rel))
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if part == ".organizer" || part == ".trash" {
				return fmt.Errorf("protected directory")
			}
			if !current.ShowHidden && strings.HasPrefix(part, ".") && part != "." {
				return fmt.Errorf("hidden paths are disabled")
			}
		}
		// Check both forms: rules can distinguish files from directories.
		if !state.AccessPermitted(idx.Path, utils.IndexPathFromNormalized(full, true), current.Username) || !state.AccessPermitted(idx.Path, utils.IndexPathFromNormalized(full, false), current.Username) {
			return fmt.Errorf("path access denied")
		}
		return nil
	}
	if err = location.Check(clean); err != nil {
		return organizer.Location{}, err
	}
	return location, nil
}

func executeOrganizerItem(owner uint64, kind string, item organizer.Item, dest organizer.Destination, trash *organizer.Trash, payload string) error {
	permission := "read"
	if kind == "move" {
		permission = "move"
	}
	if kind == "trash" {
		permission = "trash"
	}
	if kind == "restore" {
		permission = "create"
	}
	src, err := organizerLocation(owner, item.Source, item.Path, permission)
	if err != nil {
		return err
	}
	if item.Binding == "" || organizerBinding(src) != item.Binding {
		return fmt.Errorf("source scope changed since job submission")
	}
	if src.Relative == "." {
		return fmt.Errorf("cannot operate on a source root")
	}
	if item.SHA256 != "" || item.Keeper != nil {
		if kind != "trash" && kind != "move" && kind != "copy" {
			return fmt.Errorf("duplicate guard requires move, copy or trash")
		}
		if item.Keeper == nil || len(item.SHA256) != 64 {
			return fmt.Errorf("duplicate cleanup requires a verified keeper")
		}
		if _, err := hex.DecodeString(item.SHA256); err != nil {
			return fmt.Errorf("invalid duplicate checksum")
		}
		src.Verify = func() error {
			original, err := organizerLocation(owner, item.Source, item.Path, "read")
			if err != nil {
				return err
			}
			if organizerBinding(original) != item.Binding {
				return fmt.Errorf("duplicate candidate scope changed")
			}
			keeper, err := organizerLocation(owner, item.Keeper.Source, item.Keeper.Path, "read")
			if err != nil {
				return err
			}
			if item.Keeper.Binding == "" || organizerBinding(keeper) != item.Keeper.Binding {
				return fmt.Errorf("keeper scope changed")
			}
			return verifyOrganizerDuplicate(original, keeper, item.SHA256)
		}
	}
	var target organizer.Location
	switch kind {
	case "move", "copy":
		target, err = organizerLocation(owner, dest.Source, path.Join(dest.Path, path.Base(item.Path)), "create")
		if err != nil {
			return err
		}
		if dest.Binding == "" || organizerBinding(target) != dest.Binding {
			return fmt.Errorf("destination scope changed since job submission")
		}
	case "trash":
		target = organizer.Location{Root: filepath.Dir(payload), Relative: filepath.Base(payload)}
	case "restore":
		if trash == nil || trash.Owner != owner || trash.Source != item.Source || trash.Path != item.Path {
			return fmt.Errorf("trash item unavailable")
		}
		target = src
		src = organizer.Location{Root: filepath.Dir(payload), Relative: filepath.Base(payload)}
	default:
		return fmt.Errorf("invalid operation")
	}
	st, err := os.Lstat(filepath.Join(src.Root, src.Relative))
	if err != nil {
		return err
	}
	isDir := st.IsDir()
	if err = organizer.Transfer(src, target, kind != "copy"); err != nil {
		return err
	}
	srcReal := filepath.Join(src.Root, src.Relative)
	dstReal := filepath.Join(target.Root, target.Relative)
	// Keep the existing source indexes and moved share references coherent.
	if kind == "move" {
		reconcileSharesAfterMove(isDir, item.Source, dest.Source, srcReal, dstReal)
	}
	if kind == "move" || kind == "trash" {
		if idx := indexing.GetIndex(item.Source); idx != nil {
			idx.DeleteMetadata(idx.MakeIndexPath(srcReal, isDir).String(), isDir, isDir)
			indexing.RealPathCache.Delete(srcReal)
			indexing.IsDirCache.Delete(srcReal + ":isdir")
			_ = files.RefreshIndex(item.Source, filepath.Dir(srcReal), true, false)
		}
	}
	if kind == "copy" || kind == "move" {
		_ = files.RefreshIndex(dest.Source, dstReal, isDir, isDir)
	}
	if kind == "restore" {
		_ = files.RefreshIndex(item.Source, dstReal, isDir, isDir)
	}
	return nil
}

func organizerHandler(w http.ResponseWriter, r *http.Request, d *Context) (int, error) {
	if d == nil || d.User == nil || d.User.ID == 0 || d.Share.Hash != "" {
		return http.StatusForbidden, fmt.Errorf("authenticated account required")
	}
	// Durable jobs intentionally require an interactive account session. Named
	// API token caps must never turn into the owner's full background authority.
	if d.Token != "" {
		owner, session, ok := state.HashedTokenOwner(d.Token)
		if !ok || !session || owner != d.User.ID {
			return http.StatusForbidden, fmt.Errorf("organizer requires an account session")
		}
	}
	if err := InitOrganizer(); err != nil {
		return http.StatusServiceUnavailable, err
	}
	organizerService.Lock()
	m := organizerService.manager
	organizerService.Unlock()
	// The main router strips the configured /api prefix before dispatch.
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/organizer/"), "/"), "/")
	owner := d.User.ID
	if len(parts) == 1 && parts[0] == "trash" && r.Method == http.MethodGet {
		visible := []organizer.Trash{}
		for _, t := range m.Trash(owner) {
			if _, err := organizerLocation(owner, t.Source, t.Path, "create"); err == nil {
				visible = append(visible, t)
			}
		}
		return RenderJSON(w, r, visible)
	}
	if len(parts) == 1 && parts[0] == "jobs" {
		switch r.Method {
		case http.MethodGet:
			return RenderJSON(w, r, m.List(owner))
		case http.MethodPost:
			var req organizer.Request
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				return http.StatusBadRequest, fmt.Errorf("invalid organizer request")
			}
			// Destination traversal must be rejected before path.Join normalizes it.
			if req.Kind == "move" || req.Kind == "copy" {
				if _, err := organizer.CleanRelative(req.Destination.Path); err != nil {
					return http.StatusBadRequest, err
				}
				location, err := organizerLocation(owner, req.Destination.Source, req.Destination.Path, "create")
				if err != nil {
					return http.StatusForbidden, err
				}
				req.Destination.Binding = organizerBinding(location)
			}
			if req.Kind != "restore" {
				for i, it := range req.Items {
					perm := "read"
					if req.Kind == "move" {
						perm = "move"
					}
					if req.Kind == "trash" {
						perm = "trash"
					}
					location, err := organizerLocation(owner, it.Source, it.Path, perm)
					if err != nil {
						return http.StatusForbidden, err
					}
					req.Items[i].Binding = organizerBinding(location)
					if it.Keeper != nil {
						keeper, err := organizerLocation(owner, it.Keeper.Source, it.Keeper.Path, "read")
						if err != nil {
							return http.StatusForbidden, err
						}
						req.Items[i].Keeper.Binding = organizerBinding(keeper)
						for _, selected := range req.Items {
							selectedLoc, err := organizerLocation(owner, selected.Source, selected.Path, perm)
							if err != nil {
								return http.StatusForbidden, err
							}
							if err = organizerKeeperOutsideSelection(keeper, selectedLoc); err != nil {
								return http.StatusBadRequest, err
							}
						}
					}
				}
			}
			job, err := m.Submit(owner, req)
			if err != nil {
				return http.StatusBadRequest, err
			}
			return RenderJSON(w, r, job, http.StatusAccepted)
		}
	}
	if len(parts) >= 2 && len(parts) <= 3 && parts[0] == "jobs" {
		var job organizer.Job
		var err error
		if len(parts) == 2 && r.Method == http.MethodGet {
			job, err = m.Get(owner, parts[1])
		} else if len(parts) == 3 && r.Method == http.MethodPost && (parts[2] == "cancel" || parts[2] == "retry") {
			job, err = m.Change(owner, parts[1], parts[2])
		} else {
			return http.StatusMethodNotAllowed, fmt.Errorf("method not supported")
		}
		if errors.Is(err, organizer.ErrNotFound) {
			return http.StatusNotFound, err
		}
		if err != nil {
			return http.StatusBadRequest, err
		}
		return RenderJSON(w, r, job)
	}
	return http.StatusNotFound, fmt.Errorf("organizer endpoint not found")
}

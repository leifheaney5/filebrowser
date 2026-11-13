package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/gtsteffaniak/filebrowser/backend/internal/database/users"
	"github.com/gtsteffaniak/filebrowser/backend/internal/library"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
)

var libraryServiceMu sync.Mutex
var libraryService *library.Service

// initializeLibrary also resumes durable scans. Call after source/user state has
// been initialized at server startup; requests retry initialization on failure.
func initializeLibrary() error {
	libraryServiceMu.Lock()
	defer libraryServiceMu.Unlock()
	if libraryService != nil {
		return nil
	}
	if settings.Config.Server.DatabaseV2.Path == "" {
		return fmt.Errorf("library requires a persistent database path")
	}
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(settings.Config.Server.DatabaseV2.Path), "organizer"))
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
	for _, source := range settings.Config.Server.Sources {
		root, e := filepath.Abs(source.Path)
		if e != nil {
			return e
		}
		if actual, e := filepath.EvalSymlinks(root); e == nil {
			root = actual
		}
		rel, e := filepath.Rel(root, dir)
		if e == nil && (rel == "." || filepath.IsLocal(rel)) {
			return fmt.Errorf("library data directory must be outside all file sources")
		}
	}
	service, err := library.Open(filepath.Join(dir, "library.sqlite"), resolveLibraryPath)
	if err != nil {
		return err
	}
	libraryService = service
	return nil
}

func libraryPrincipal(d *Context, sourceName string) (library.Principal, error) {
	p := library.Principal{}
	if d == nil || d.User == nil || d.User.ID == 0 || d.Share.Hash != "" {
		return p, library.ErrDenied
	}
	source, ok := users.ResolveSourceKey(sourceName)
	if !ok {
		return p, library.ErrDenied
	}
	scope, err := d.User.GetScopeForSourcePath(source.Path)
	if err != nil {
		return p, library.ErrDenied
	}
	perms, err := effectiveFilePerms(d, source.Name)
	if err != nil || !perms.View || !perms.Download {
		return p, library.ErrDenied
	}
	p = library.Principal{UserID: d.User.ID, SourceRoot: source.Path, Scope: scope, View: perms.View, Download: perms.Download}
	if d.Token != "" {
		// Store a name/expiry permission ceiling, never a reusable bearer token.
		ownerID, session, valid := state.HashedTokenOwner(d.Token)
		if !valid || !session || ownerID != p.UserID {
			return library.Principal{}, library.ErrDenied
		}
		var claims users.AuthToken
		if _, _, err := new(jwt.Parser).ParseUnverified(d.Token, &claims); err != nil {
			return library.Principal{}, library.ErrDenied
		}
		if claims.RegisteredClaims.ExpiresAt != nil {
			p.ExpiresAt = claims.RegisteredClaims.ExpiresAt.Unix()
		}
	}
	return p, nil
}

func safeLibraryVirtual(raw string) (string, error) {
	if strings.ContainsAny(raw, "\x00\\") {
		return "", library.ErrDenied
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." || part == ".trash" || part == ".organizer" {
			return "", library.ErrDenied
		}
	}
	return path.Clean("/" + strings.TrimPrefix(raw, "/")), nil
}

// resolveLibraryPath checks live authorization on every use, including cached
// reads and each scan file. Scope changes invalidate the old coordinate system.
// Every path component is inspected without following symlinks.
func resolveLibraryPath(p library.Principal, sourceName, virtual string) (library.Resolved, error) {
	denied := func() (library.Resolved, error) { return library.Resolved{}, library.ErrDenied }
	if !p.View || !p.Download || p.UserID == 0 || (p.ExpiresAt > 0 && time.Now().Unix() >= p.ExpiresAt) {
		return denied()
	}
	u, err := state.GetUserByID(p.UserID)
	if err != nil {
		return denied()
	}
	if p.TokenName != "" {
		token, ok := u.Tokens[p.TokenName]
		if !ok {
			return denied()
		}
		if token.ExpiresAt > 0 && time.Now().Unix() >= token.ExpiresAt {
			return denied()
		}
		if token.RegisteredClaims.ExpiresAt != nil && time.Now().Unix() >= token.RegisteredClaims.ExpiresAt.Unix() {
			return denied()
		}
		if users.HasAnyGlobalPermission(token.Permissions) && !token.Permissions.Api {
			return denied()
		}
	}
	source, ok := users.ResolveSourceKey(sourceName)
	if !ok || source.Path != p.SourceRoot {
		return denied()
	}
	scope, err := u.GetScopeForSourcePath(source.Path)
	if err != nil || scope != p.Scope {
		return denied()
	}
	perms, ok := u.FilePermsForSourcePath(source.Path)
	if !ok || !perms.View || !perms.Download {
		return denied()
	}
	virtual, err = safeLibraryVirtual(virtual)
	if err != nil {
		return denied()
	}
	idx := indexing.GetIndex(source.Name)
	if idx == nil {
		return denied()
	}
	indexPath := idx.MakeIndexPath(utils.JoinScopedIndexPath(scope, virtual), false)
	if !state.AccessPermitted(source.Path, indexPath, u.Username) {
		return denied()
	}
	root, err := filepath.Abs(source.Path)
	if err != nil {
		return denied()
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return denied()
	}
	combined := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(utils.JoinScopedIndexPath(scope, virtual), "/")))
	rel, err := filepath.Rel(root, combined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return denied()
	}
	privateDir, _ := filepath.Abs(filepath.Join(filepath.Dir(settings.Config.Server.DatabaseV2.Path), "organizer"))
	if combined == privateDir || strings.HasPrefix(combined, privateDir+string(filepath.Separator)) {
		return denied()
	}
	current := root
	info, err := os.Lstat(root)
	if err != nil {
		return library.Resolved{}, err
	}
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err = os.Lstat(current)
			if err != nil {
				return library.Resolved{}, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return denied()
			}
		}
	}
	if !idx.IsViewableWithParentCheck(info.IsDir(), indexPath.String(), false, indexing.IsHidden(combined)) {
		return denied()
	}
	return library.Resolved{Absolute: combined, Info: info, Open: func() (*os.File, error) {
		// Root.Open cannot escape the configured source even if an ancestor is
		// exchanged for a symlink after Lstat. The engine verifies file identity.
		anchor, e := os.OpenRoot(root)
		if e != nil {
			return nil, e
		}
		defer anchor.Close()
		return anchor.Open(rel)
	}}, nil
}

func libraryHandler(w http.ResponseWriter, r *http.Request, d *Context) (int, error) {
	var body struct {
		Source string   `json:"source"`
		Path   string   `json:"path"`
		Tags   []string `json:"tags"`
	}
	sourceName := r.URL.Query().Get("source")
	virtual := r.URL.Query().Get("path")
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		defer r.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			return http.StatusBadRequest, fmt.Errorf("invalid library request: %w", err)
		}
		sourceName = body.Source
		virtual = body.Path
	}
	cleanPath, err := safeLibraryVirtual(virtual)
	if err != nil {
		return http.StatusForbidden, err
	}
	p, err := libraryPrincipal(d, sourceName)
	if err != nil {
		return http.StatusForbidden, err
	}
	if _, err = resolveLibraryPath(p, sourceName, cleanPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return http.StatusNotFound, err
		}
		return http.StatusForbidden, err
	}
	if err = initializeLibrary(); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("library storage unavailable: %w", err)
	}
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/library/scans"):
		scan, err := libraryService.Start(p, sourceName, cleanPath)
		if err != nil {
			if errors.Is(err, library.ErrDenied) {
				return http.StatusForbidden, err
			}
			return http.StatusConflict, err
		}
		return RenderJSON(w, r, scan)
	case r.Method == http.MethodPatch && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/library/tags"):
		tags, err := libraryService.SetTags(p, sourceName, cleanPath, body.Tags)
		if err != nil {
			return http.StatusBadRequest, err
		}
		return RenderJSON(w, r, map[string]any{"tags": tags})
	case r.Method == http.MethodGet:
		offset, err := libraryInt(r, "offset", 0, 0, 1000000)
		if err != nil {
			return http.StatusBadRequest, err
		}
		limit, err := libraryInt(r, "limit", 100, 1, 500)
		if err != nil {
			return http.StatusBadRequest, err
		}
		result, err := libraryService.List(p, library.Query{Source: sourceName, Path: cleanPath, Search: r.URL.Query().Get("q"), Sort: r.URL.Query().Get("sort"), Direction: r.URL.Query().Get("direction"), Offset: offset, Limit: limit})
		if err != nil {
			if errors.Is(err, library.ErrDenied) {
				return http.StatusForbidden, err
			}
			return http.StatusInternalServerError, err
		}
		return RenderJSON(w, r, result)
	default:
		return http.StatusMethodNotAllowed, fmt.Errorf("unsupported library operation")
	}
}
func libraryInt(r *http.Request, key string, fallback, min, max int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return n, nil
}

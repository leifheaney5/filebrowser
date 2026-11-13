package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/internal/database/users"
	"github.com/gtsteffaniak/filebrowser/backend/internal/library"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
)

func libraryWebFixture(t *testing.T) (string, *users.User, *Context) {
	t.Helper()
	saved := settings.Config
	t.Cleanup(func() { settings.Config = saved; settings.InitializeUserResolvers() })
	setupTestEnv(t)
	root := t.TempDir()
	source := &settings.Source{Name: "library-test", Path: root}
	settings.Config.Server.Sources = []*settings.Source{source}
	settings.Config.Server.SourceMap = map[string]*settings.Source{root: source}
	settings.Config.Server.NameToSource = map[string]*settings.Source{source.Name: source}
	settings.Config.Server.DatabaseV2.Path = filepath.Join(t.TempDir(), "filebrowser.sqlite")
	settings.InitializeUserResolvers()
	indexing.SetTestIndex(source.Name, root)
	t.Cleanup(indexing.ClearTestIndices)
	u := createTokenAuthUser(t, "library-reader", users.Permissions{Api: true})
	u.BackendScopes = []users.BackendScope{{Path: root, Scope: "/scoped", Permissions: users.SourceFilePermissions{Configured: true, View: true, Download: true}}}
	if err := state.UpdateUser(u, "", "backendScopes"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scoped"), 0700); err != nil {
		t.Fatal(err)
	}
	stored, err := state.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	u = &stored
	t.Cleanup(func() {
		libraryServiceMu.Lock()
		defer libraryServiceMu.Unlock()
		if libraryService != nil {
			_ = libraryService.Close()
			libraryService = nil
		}
	})
	return root, u, &Context{User: u}
}

func TestLibraryHandlerScopeRevocationAndNamedTokenDenial(t *testing.T) {
	root, u, d := libraryWebFixture(t)
	filename := filepath.Join(root, "scoped", "visible.txt")
	if err := os.WriteFile(filename, []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := libraryPrincipal(d, "library-test")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveLibraryPath(p, "library-test", "/visible.txt")
	if err != nil || resolved.Absolute != filename {
		t.Fatalf("scope escaped or failed: %+v %v", resolved, err)
	}
	for _, name := range []string{"../outside.txt", "/../outside.txt", "/.trash/item", "/nested/../../outside.txt", `\outside.txt`} {
		if _, err := resolveLibraryPath(p, "library-test", name); !errors.Is(err, library.ErrDenied) {
			t.Fatalf("unsafe path accepted: %q %v", name, err)
		}
	}
	if _, err := resolveLibraryPath(p, "library-test", "/outside.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absolute virtual path bypassed scope: %v", err)
	}
	status, err := libraryHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/library/scans", strings.NewReader(`{"source":"library-test","path":"/"}`)), d)
	if err != nil || status != http.StatusOK {
		t.Fatalf("scan handler: %d %v", status, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var result library.Result
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		status, err = libraryHandler(rec, httptest.NewRequest(http.MethodGet, "/library?source=library-test&path=%2F", nil), d)
		if err != nil || status != http.StatusOK {
			t.Fatalf("listing handler: %d %v", status, err)
		}
		if err = json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Scan != nil && result.Scan.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result.Total != 1 || result.Items[0].Path != "/visible.txt" {
		t.Fatalf("unexpected scoped index: %+v", result)
	}
	if err = state.DenyUser(root, utils.IndexPathFromNormalized("/scoped/visible.txt", false), u.Username); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	status, err = libraryHandler(rec, httptest.NewRequest(http.MethodGet, "/library?source=library-test&path=%2F", nil), d)
	if err != nil || status != http.StatusOK {
		t.Fatalf("redaction listing: %d %v", status, err)
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Duplicates) != 0 {
		t.Fatal("access-revoked cached file leaked")
	}
	// API credentials cannot submit durable work under their owner's session authority.
	if err = state.AddApiToken("named-token", u.ID); err != nil {
		t.Fatal(err)
	}
	tokenContext := *d
	tokenContext.Token = "named-token"
	if _, err = libraryPrincipal(&tokenContext, "library-test"); !errors.Is(err, library.ErrDenied) {
		t.Fatal("named token was elevated to owner")
	}
	// An old request user object cannot retain permissions after live revocation.
	u.BackendScopes[0].Permissions.Download = false
	if err = state.UpdateUser(u, "", "backendScopes"); err != nil {
		t.Fatal(err)
	}
	status, err = libraryHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/library?source=library-test&path=%2F", nil), d)
	if status != http.StatusForbidden || err == nil {
		t.Fatalf("live permission revocation not applied: %d %v", status, err)
	}
}

func TestLibraryHandlerRejectsSymlinksAndInvalidInput(t *testing.T) {
	root, _, d := libraryWebFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "scoped", "link")); err == nil {
		p, err := libraryPrincipal(d, "library-test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = resolveLibraryPath(p, "library-test", "/link"); !errors.Is(err, library.ErrDenied) {
			t.Fatal("symlink accepted")
		}
	} else {
		t.Logf("symlink unsupported: %v", err)
	}
	for _, target := range []string{"/library?source=library-test&offset=-1", "/library?source=library-test&limit=501", "/library?source=library-test&path=" + url.QueryEscape("../")} {
		status, err := libraryHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil), d)
		if err == nil || (status != http.StatusBadRequest && status != http.StatusForbidden) {
			t.Fatalf("invalid query accepted: %s %d %v", target, status, err)
		}
	}
	status, err := libraryHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPatch, "/library/tags", strings.NewReader(`{"source":"library-test","path":"/","unknown":true}`)), d)
	if err == nil || status != http.StatusBadRequest {
		t.Fatal("unknown body field accepted")
	}
}

package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/internal/auth"
	"github.com/gtsteffaniak/filebrowser/backend/internal/database/share"
	"github.com/gtsteffaniak/filebrowser/backend/internal/database/users"
	"github.com/gtsteffaniak/filebrowser/backend/internal/organizer"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
)

func localOrganizerLocation(root, name string) organizer.Location {
	return organizer.Location{Root: root, Relative: name, Check: func(string) error { return nil }, Open: func() (*os.Root, error) { return os.OpenRoot(root) }}
}

func TestOrganizerDuplicateGuardFreshContentsAndKeeper(t *testing.T) {
	dir := t.TempDir()
	a, b := localOrganizerLocation(dir, "candidate"), localOrganizerLocation(dir, "keeper")
	data := make([]byte, 65537)
	data[len(data)-1] = 'a'
	expected := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, name := range []string{"candidate", "keeper"} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyOrganizerDuplicate(a, b, expected); err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = 'b'
	if err := os.WriteFile(filepath.Join(dir, "keeper"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyOrganizerDuplicate(a, b, expected); err == nil {
		t.Fatal("last-byte mismatch accepted")
	}
	if err := os.Remove(filepath.Join(dir, "keeper")); err != nil {
		t.Fatal(err)
	}
	if err := verifyOrganizerDuplicate(a, b, expected); err == nil {
		t.Fatal("missing keeper accepted")
	}
	if err := os.Link(filepath.Join(dir, "candidate"), filepath.Join(dir, "keeper")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := verifyOrganizerDuplicate(a, b, expected); err == nil {
		t.Fatal("hard links treated as separate copies")
	}
}

func TestOrganizerDuplicateGuardRechecksKeeperBeforeRemoval(t *testing.T) {
	dir, target := t.TempDir(), t.TempDir()
	for _, name := range []string{"candidate", "keeper"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	candidate, keeper := localOrganizerLocation(dir, "candidate"), localOrganizerLocation(dir, "keeper")
	expected := fmt.Sprintf("%x", sha256.Sum256([]byte("same")))
	calls := 0
	candidate.Verify = func() error {
		calls++
		if calls == 2 {
			if err := os.Remove(filepath.Join(dir, "keeper")); err != nil {
				return err
			}
		}
		return verifyOrganizerDuplicate(candidate, keeper, expected)
	}
	err := organizer.Transfer(candidate, localOrganizerLocation(target, "candidate"), true)
	if err == nil || calls != 2 {
		t.Fatalf("missing keeper was accepted: %v checks %d", err, calls)
	}
	if _, statErr := os.Stat(filepath.Join(target, "candidate")); statErr == nil && !errors.Is(err, organizer.ErrReview) {
		t.Fatalf("partial copy did not require review: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "candidate")); err != nil {
		t.Fatal("sole surviving original deleted")
	}
}

func TestOrganizerKeeperCannotBeSelected(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder/keeper"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	keeper := localOrganizerLocation(dir, "folder/keeper")
	for _, selected := range []string{"folder/keeper", "folder"} {
		if err := organizerKeeperOutsideSelection(keeper, localOrganizerLocation(dir, selected)); err == nil {
			t.Errorf("keeper accepted inside selected %s", selected)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "other"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := organizerKeeperOutsideSelection(keeper, localOrganizerLocation(dir, "other")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "folder/keeper"), filepath.Join(dir, "alias")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := organizerKeeperOutsideSelection(keeper, localOrganizerLocation(dir, "alias")); err == nil {
		t.Fatal("selected keeper hardlink accepted")
	}
}

func TestOrganizerRejectsAnonymousSharesAndNamedTokens(t *testing.T) {
	setupTestEnv(t)
	user := createTokenAuthUser(t, "organizer-owner", users.Permissions{Admin: true, Api: true})
	named, _, err := auth.MakeSignedTokenAPI(user, "organizer-test-key", time.Hour, users.Permissions{Api: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = state.AddApiToken(named, user.ID); err != nil {
		t.Fatal(err)
	}
	cases := []*Context{nil, {}, {User: &users.User{}}, {User: user, Share: share.Share{Hash: "public"}}, {User: user, Token: named}, {User: user, Token: "unregistered"}}
	for n, ctx := range cases {
		status, err := organizerHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/organizer/jobs", nil), ctx)
		if status != http.StatusForbidden || err == nil {
			t.Fatalf("case %d got %d %v", n, status, err)
		}
	}
}

func TestOrganizerRoutesAndUserIsolation(t *testing.T) {
	manager, err := organizer.Open(t.TempDir(), func(uint64, string, organizer.Item, organizer.Destination, *organizer.Trash, string) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	organizerService.Lock()
	old := organizerService.manager
	organizerService.manager = manager
	organizerService.Unlock()
	defer func() {
		manager.Close()
		organizerService.Lock()
		organizerService.manager = old
		organizerService.Unlock()
	}()
	job, err := manager.Submit(42, organizer.Request{Kind: "copy", Items: []organizer.Item{{Source: "s", Path: "file"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []uint64{42, 43} {
		ctx := &Context{User: &users.User{ID: owner}}
		rec := httptest.NewRecorder()
		status, err := organizerHandler(rec, httptest.NewRequest(http.MethodGet, "/organizer/jobs", nil), ctx)
		if err != nil || status != http.StatusOK {
			t.Fatalf("list got %d %v", status, err)
		}
		var jobs []organizer.Job
		if err = json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
			t.Fatal(err)
		}
		if (owner == 42 && len(jobs) != 1) || (owner == 43 && len(jobs) != 0) {
			t.Fatalf("job leak owner %d: %+v", owner, jobs)
		}
	}
	status, err := organizerHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/organizer/jobs/"+job.ID+"/cancel", nil), &Context{User: &users.User{ID: 43}})
	if status != http.StatusNotFound || err == nil {
		t.Fatal("another account cancelled job")
	}
}

func TestOrganizerExpectedHashContract(t *testing.T) {
	var item organizer.Item
	if err := json.Unmarshal([]byte(`{"source":"s","path":"a","expectedSha256":"abc","keeper":{"source":"s","path":"b"}}`), &item); err != nil {
		t.Fatal(err)
	}
	if item.SHA256 != "abc" || item.Keeper == nil || item.Keeper.Path != "b" {
		t.Fatal("UI duplicate safety fields discarded")
	}
}

func TestOrganizerNativeHTTPCopyAndAccessRules(t *testing.T) {
	previousConfig := settings.Config
	defer func() { settings.Config = previousConfig; settings.InitializeUserResolvers() }()
	setupTestEnv(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "copies"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("native path payload"), 0600); err != nil {
		t.Fatal(err)
	}
	source := &settings.Source{Name: "organizer-native", Path: root}
	settings.Config.Server.SourceMap = map[string]*settings.Source{root: source}
	settings.Config.Server.NameToSource = map[string]*settings.Source{source.Name: source}
	settings.InitializeUserResolvers()
	indexing.SetTestIndex(source.Name, root)
	defer indexing.ClearTestIndices()
	indexing.GetIndex(source.Name).Config.ResolvedRules.IndexingDisabled = true
	perms := users.SourceFilePermissions{View: true, Download: true, Modify: true, Delete: true, Create: true, Configured: true}
	user := &users.User{FrontendUser: users.FrontendUser{Username: "native-organizer"}, BackendScopes: []users.BackendScope{{Path: root, Scope: filepath.FromSlash("/"), Permissions: perms}}, BackendSourcePermissions: map[string]users.SourceFilePermissions{root: perms}, Version: users.SourcePermissionsMigrationVersion}
	if err := state.CreateUser(user, ""); err != nil {
		t.Fatal(err)
	}
	manager, err := organizer.Open(t.TempDir(), executeOrganizerItem)
	if err != nil {
		t.Fatal(err)
	}
	organizerService.Lock()
	old := organizerService.manager
	organizerService.manager = manager
	organizerService.Unlock()
	defer func() {
		manager.Close()
		organizerService.Lock()
		organizerService.manager = old
		organizerService.Unlock()
	}()
	body := fmt.Sprintf(`{"kind":"copy","items":[{"source":%q,"path":"/report.txt"}],"destination":{"source":%q,"path":"/copies"}}`, source.Name, source.Name)
	rec := httptest.NewRecorder()
	status, err := organizerHandler(rec, httptest.NewRequest(http.MethodPost, "/organizer/jobs", bytes.NewBufferString(body)), &Context{User: user})
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("native scope copy got %d: %v", status, err)
	}
	var job organizer.Job
	if err = json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err = manager.Get(user.ID, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != "running" && job.Status != "queued" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("copy did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if job.Status != "completed" {
		t.Fatalf("native copy failed: %+v", job)
	}
	data, err := os.ReadFile(filepath.Join(root, "copies/report.txt"))
	if err != nil || string(data) != "native path payload" {
		t.Fatalf("copy content %q %v", data, err)
	}
	if err = state.DenyAll(root, utils.IndexPathFromNormalized("/report.txt", false)); err != nil {
		t.Fatal(err)
	}
	status, err = organizerHandler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/organizer/jobs", bytes.NewBufferString(body)), &Context{User: user})
	if status != http.StatusForbidden || err == nil {
		t.Fatalf("path access rule ignored: %d %v", status, err)
	}
}

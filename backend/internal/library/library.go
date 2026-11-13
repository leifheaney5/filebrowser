// Package library maintains a private, durable, read-only content index. It never
// removes files: duplicate groups are evidence for a user's explicit review.
package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/internal/database/sqldb"
)

var ErrDenied = errors.New("library access denied")

// Principal is a permission ceiling captured at submission. The resolver must
// intersect it with current permissions, never replace it with owner rights.
type Principal struct {
	UserID     uint64 `json:"userId"`
	SourceRoot string `json:"sourceRoot"`
	Scope      string `json:"scope"`
	View       bool   `json:"view"`
	Download   bool   `json:"download"`
	TokenName  string `json:"tokenName,omitempty"`
	ExpiresAt  int64  `json:"expiresAt,omitempty"`
}

type Resolved struct {
	Absolute string
	Info     os.FileInfo
	// Open anchors reads to a previously checked root. Web resolvers provide it
	// so pathname races cannot redirect a scan outside its authorized scope.
	Open func() (*os.File, error)
}
type Resolver func(Principal, string, string) (Resolved, error)

type Metadata struct {
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	CapturedAt string  `json:"capturedAt,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	Artist     string  `json:"artist,omitempty"`
	Album      string  `json:"album,omitempty"`
	Title      string  `json:"title,omitempty"`
}
type Item struct {
	Source   string    `json:"source"`
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Type     string    `json:"type"`
	Metadata Metadata  `json:"metadata"`
	Tags     []string  `json:"tags"`
	SHA256   string    `json:"sha256"`
}
type Duplicate struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Files  []Item `json:"files"`
}
type Scan struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Path      string    `json:"path"`
	Status    string    `json:"status"`
	Processed int       `json:"processed"`
	Errors    int       `json:"errors"`
	Started   time.Time `json:"started"`
	Updated   time.Time `json:"updated"`
	Error     string    `json:"error,omitempty"`
	Principal Principal `json:"-"`
}
type Query struct {
	Source, Path, Search, Sort, Direction string
	Offset, Limit                         int
}
type Result struct {
	Items      []Item      `json:"items"`
	Total      int         `json:"total"`
	Scan       *Scan       `json:"scan"`
	Duplicates []Duplicate `json:"duplicates"`
}
type Service struct {
	db         *sql.DB
	resolve    Resolver
	ctx        context.Context
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	mu         sync.Mutex
	closed     bool
	wake       chan struct{}
	privateDir string
}

func Open(filename string, resolve Resolver) (*Service, error) {
	if resolve == nil {
		return nil, errors.New("library resolver required")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open(sqldb.SqliteDriver, filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS library_scans(id TEXT PRIMARY KEY, owner TEXT NOT NULL, source TEXT NOT NULL, path TEXT NOT NULL, principal TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS library_scan_owner ON library_scans(owner,source,path);
CREATE TABLE IF NOT EXISTS library_items(owner TEXT NOT NULL, source TEXT NOT NULL, path TEXT NOT NULL, fingerprint TEXT NOT NULL, data TEXT NOT NULL, scan TEXT NOT NULL, PRIMARY KEY(owner,source,path));
CREATE INDEX IF NOT EXISTS library_hash_cache ON library_items(fingerprint);
CREATE TABLE IF NOT EXISTS library_tags(owner TEXT NOT NULL, source TEXT NOT NULL, path TEXT NOT NULL, tags TEXT NOT NULL, PRIMARY KEY(owner,source,path));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(filename, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	privateDir, _ := filepath.Abs(filepath.Dir(filename))
	s := &Service{db: db, resolve: resolve, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), privateDir: privateDir}
	s.workers.Add(1)
	go s.worker()
	s.signal()
	return s, nil
}
func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.workers.Wait()
	return s.db.Close()
}
func owner(p Principal) string { return strconv.FormatUint(p.UserID, 10) }
func clean(p string) string    { return path.Clean("/" + strings.TrimPrefix(p, "/")) }
func within(parent, child string) bool {
	parent = clean(parent)
	child = clean(child)
	return parent == "/" || child == parent || strings.HasPrefix(child, parent+"/")
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) saveScan(scan Scan) error {
	data, _ := json.Marshal(scan)
	principal, _ := json.Marshal(scan.Principal)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO library_scans(id,owner,source,path,principal,data) VALUES(?,?,?,?,?,?)`, scan.ID, owner(scan.Principal), scan.Source, scan.Path, string(principal), string(data))
	return err
}
func (s *Service) Start(p Principal, source, root string) (Scan, error) {
	root = clean(root)
	resolved, err := s.resolve(p, source, root)
	if err != nil {
		return Scan{}, err
	}
	if !resolved.Info.IsDir() {
		return Scan{}, errors.New("scan path must be a directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Scan{}, errors.New("library closed")
	}
	// One queued/running scan per user prevents repeated clicks creating a disk-I/O backlog.
	rows, err := s.db.Query(`SELECT data,principal FROM library_scans WHERE owner=?`, owner(p))
	if err != nil {
		return Scan{}, err
	}
	for rows.Next() {
		var raw, pr string
		var old Scan
		if err = rows.Scan(&raw, &pr); err != nil {
			rows.Close()
			return Scan{}, err
		}
		if json.Unmarshal([]byte(raw), &old) == nil && (old.Status == "queued" || old.Status == "running") {
			rows.Close()
			return Scan{}, errors.New("a library scan is already in progress")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return Scan{}, err
	}
	rows.Close()
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return Scan{}, err
	}
	now := time.Now().UTC()
	scan := Scan{ID: hex.EncodeToString(b), Source: source, Path: root, Status: "queued", Started: now, Updated: now, Principal: p}
	if err = s.saveScan(scan); err != nil {
		return Scan{}, err
	}
	s.signal()
	return scan, nil
}
func (s *Service) pending() ([]Scan, error) {
	rows, err := s.db.Query(`SELECT data,principal FROM library_scans ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		var data, p string
		var scan Scan
		if err = rows.Scan(&data, &p); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &scan); err != nil {
			return nil, err
		}
		if scan.Status != "queued" && scan.Status != "running" {
			continue
		}
		if err = json.Unmarshal([]byte(p), &scan.Principal); err != nil {
			return nil, err
		}
		out = append(out, scan)
	}
	return out, rows.Err()
}
func (s *Service) worker() {
	defer s.workers.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			scans, err := s.pending()
			if err != nil {
				continue
			}
			for _, scan := range scans {
				if s.ctx.Err() != nil {
					return
				}
				s.scan(scan)
			}
		}
	}
}

func identity(info os.FileInfo) string {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		dev, ino := v.FieldByName("Dev"), v.FieldByName("Ino")
		if dev.IsValid() && ino.IsValid() && dev.CanUint() && ino.CanUint() {
			return fmt.Sprintf("%d:%d", dev.Uint(), ino.Uint())
		}
	}
	return ""
}
func fingerprint(filename string, info os.FileInfo) string {
	id := identity(info)
	if id == "" {
		id = filename
	}
	return fmt.Sprintf("%s:%d:%d", id, info.Size(), info.ModTime().UnixNano())
}
func reserved(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".trash" || part == ".organizer" {
			return true
		}
	}
	return false
}
func (s *Service) scan(scan Scan) {
	scan.Status = "running"
	scan.Processed = 0
	scan.Errors = 0
	scan.Error = ""
	scan.Updated = time.Now().UTC()
	if s.saveScan(scan) != nil {
		return
	}
	root, err := s.resolve(scan.Principal, scan.Source, scan.Path)
	if err != nil {
		scan.Status = "failed"
		scan.Error = "scan access is no longer permitted"
		scan.Updated = time.Now().UTC()
		_ = s.saveScan(scan)
		return
	}
	seen := map[string]bool{}
	fallbackSeen := map[int64][]os.FileInfo{}
	err = filepath.WalkDir(root.Absolute, func(filename string, entry fs.DirEntry, walkErr error) error {
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		if walkErr != nil {
			scan.Errors++
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root.Absolute, filename)
		if err != nil {
			return err
		}
		virtual := path.Join(scan.Path, filepath.ToSlash(rel))
		if reserved(virtual) || filename == s.privateDir || strings.HasPrefix(filename, s.privateDir+string(filepath.Separator)) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Revalidate the submitted root as well as each file: root revocation stops
		// the job rather than silently returning an apparently complete scan.
		if _, err = s.resolve(scan.Principal, scan.Source, scan.Path); err != nil {
			return ErrDenied
		}
		resolved, err := s.resolve(scan.Principal, scan.Source, virtual)
		if err != nil {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if resolved.Info.IsDir() {
			return nil
		}
		if !resolved.Info.Mode().IsRegular() {
			return nil
		}
		id := identity(resolved.Info)
		if id != "" {
			if seen[id] {
				return nil
			}
			seen[id] = true
		} else {
			for _, prior := range fallbackSeen[resolved.Info.Size()] {
				if os.SameFile(prior, resolved.Info) {
					return nil
				}
			}
			fallbackSeen[resolved.Info.Size()] = append(fallbackSeen[resolved.Info.Size()], resolved.Info)
		}
		item, err := s.indexFile(scan, resolved, virtual)
		if err != nil {
			if errors.Is(err, ErrDenied) {
				return err
			}
			scan.Errors++
			return nil
		}
		data, _ := json.Marshal(item)
		_, err = s.db.Exec(`INSERT OR REPLACE INTO library_items(owner,source,path,fingerprint,data,scan) VALUES(?,?,?,?,?,?)`, owner(scan.Principal), scan.Source, virtual, fingerprint(resolved.Absolute, resolved.Info), string(data), scan.ID)
		if err != nil {
			return err
		}
		scan.Processed++
		scan.Updated = time.Now().UTC()
		if scan.Processed%25 == 0 {
			return s.saveScan(scan)
		}
		return nil
	})
	if s.ctx.Err() != nil {
		return
	} // The running record resumes after a server restart.
	if err != nil {
		scan.Status = "failed"
		scan.Error = "scan interrupted: " + err.Error()
	} else {
		// Prune only this submitted subtree, preserving other independently scanned folders.
		_, err = s.db.Exec(`DELETE FROM library_items WHERE owner=? AND source=? AND scan<>? AND (path=? OR substr(path,1,?)=?)`, owner(scan.Principal), scan.Source, scan.ID, scan.Path, len(strings.TrimRight(scan.Path, "/")+"/"), strings.TrimRight(scan.Path, "/")+"/")
		if err != nil {
			scan.Status = "failed"
			scan.Error = "unable to finalize index"
		} else {
			scan.Status = "completed"
		}
	}
	scan.Updated = time.Now().UTC()
	_ = s.saveScan(scan)
}
func (s *Service) indexFile(scan Scan, resolved Resolved, virtual string) (Item, error) {
	var raw, fp string
	var item Item
	err := s.db.QueryRow(`SELECT data,fingerprint FROM library_items WHERE owner=? AND source=? AND path=?`, owner(scan.Principal), scan.Source, virtual).Scan(&raw, &fp)
	if err == nil && fp == fingerprint(resolved.Absolute, resolved.Info) && json.Unmarshal([]byte(raw), &item) == nil {
		return item, nil
	}
	// The persistent identity/size/nanosecond-mtime cache also survives renames
	// and moves on inode-bearing filesystems. Authorization was rechecked before
	// this lookup; cached metadata never grants permission to another pathname.
	key := fingerprint(resolved.Absolute, resolved.Info)
	if err = s.db.QueryRow(`SELECT data FROM library_items WHERE fingerprint=? LIMIT 1`, key).Scan(&raw); err == nil && json.Unmarshal([]byte(raw), &item) == nil && item.Type == fileType(virtual) {
		item.Source = scan.Source
		item.Path = virtual
		item.Name = path.Base(virtual)
		item.Modified = resolved.Info.ModTime().UTC()
		item.Tags = []string{}
		return item, nil
	}
	var file *os.File
	if resolved.Open != nil {
		file, err = resolved.Open()
	} else {
		file, err = os.Open(resolved.Absolute)
	}
	if err != nil {
		return item, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return item, err
	}
	if !os.SameFile(info, resolved.Info) || !info.Mode().IsRegular() {
		return item, errors.New("file changed during scan")
	}
	hash := sha256.New()
	buffer := make([]byte, 256*1024)
	lastCheck := time.Now()
	for {
		if s.ctx.Err() != nil {
			return item, s.ctx.Err()
		}
		if time.Since(lastCheck) > time.Second {
			if _, err = s.resolve(scan.Principal, scan.Source, virtual); err != nil {
				return item, ErrDenied
			}
			lastCheck = time.Now()
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return item, readErr
		}
	}
	if _, err = s.resolve(scan.Principal, scan.Source, virtual); err != nil {
		return item, ErrDenied
	}
	after, err := file.Stat()
	if err != nil {
		return item, err
	}
	if !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return item, errors.New("file changed during hashing")
	}
	item = Item{Source: scan.Source, Path: virtual, Name: path.Base(virtual), Size: info.Size(), Modified: info.ModTime().UTC(), Type: fileType(virtual), SHA256: hex.EncodeToString(hash.Sum(nil)), Tags: []string{}}
	item.Metadata = metadataFromFile(s.ctx, file, item.Type)
	// An external rename/replacement during metadata extraction cannot certify a
	// checksum for the replacement. The next scan will retry the changed file.
	current, err := s.resolve(scan.Principal, scan.Source, virtual)
	if err != nil {
		return item, ErrDenied
	}
	if !os.SameFile(info, current.Info) || info.Size() != current.Info.Size() || !info.ModTime().Equal(current.Info.ModTime()) {
		return item, errors.New("file changed during metadata extraction")
	}
	return item, nil
}

func (s *Service) SetTags(p Principal, source, filename string, tags []string) ([]string, error) {
	filename = clean(filename)
	if _, err := s.resolve(p, source, filename); err != nil {
		return nil, err
	}
	if len(tags) > 30 {
		return nil, errors.New("at most 30 tags are allowed")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if len(tag) > 80 || strings.ContainsAny(tag, "\x00\r\n") {
			return nil, errors.New("tags must be at most 80 bytes without control characters")
		}
		if tag != "" && !seen[strings.ToLower(tag)] {
			out = append(out, tag)
			seen[strings.ToLower(tag)] = true
		}
	}
	sort.Strings(out)
	data, _ := json.Marshal(out)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO library_tags(owner,source,path,tags) VALUES(?,?,?,?)`, owner(p), source, filename, string(data))
	return out, err
}

func (s *Service) List(p Principal, q Query) (Result, error) {
	result := Result{Items: []Item{}, Duplicates: []Duplicate{}}
	q.Path = clean(q.Path)
	if _, err := s.resolve(p, q.Source, q.Path); err != nil {
		return result, err
	}
	rows, err := s.db.Query(`SELECT i.data,i.fingerprint,COALESCE(t.tags,'[]') FROM library_items i LEFT JOIN library_tags t ON t.owner=i.owner AND t.source=i.source AND t.path=i.path WHERE i.owner=? AND i.source=?`, owner(p), q.Source)
	if err != nil {
		return result, err
	}
	type cached struct {
		item Item
		fp   string
	}
	cachedItems := []cached{}
	for rows.Next() {
		var data, fp, tags string
		var item Item
		if err = rows.Scan(&data, &fp, &tags); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal([]byte(data), &item); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal([]byte(tags), &item.Tags); err != nil {
			rows.Close()
			return result, err
		}
		if within(q.Path, item.Path) {
			cachedItems = append(cachedItems, cached{item, fp})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	items := []Item{}
	groups := map[string][]Item{}
	seenPhysical := map[string]bool{}
	seenFallback := map[int64][]os.FileInfo{}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	for _, cached := range cachedItems {
		item := cached.item
		resolved, e := s.resolve(p, item.Source, item.Path)
		if e != nil {
			continue
		}
		if cached.fp != fingerprint(resolved.Absolute, resolved.Info) {
			continue
		}
		haystack := strings.ToLower(item.Path + " " + strings.Join(item.Tags, " ") + " " + item.Metadata.Artist + " " + item.Metadata.Album + " " + item.Metadata.Title)
		if search != "" && !strings.Contains(haystack, search) {
			continue
		}
		items = append(items, item)
		if item.SHA256 != "" {
			// Separate subtree scans can encounter two names for the same inode.
			// They remain browsable but count once in duplicate reclamation groups.
			id := identity(resolved.Info)
			alreadySeen := false
			if id != "" {
				alreadySeen = seenPhysical[id]
				seenPhysical[id] = true
			} else {
				for _, prior := range seenFallback[resolved.Info.Size()] {
					if os.SameFile(prior, resolved.Info) {
						alreadySeen = true
						break
					}
				}
				seenFallback[resolved.Info.Size()] = append(seenFallback[resolved.Info.Size()], resolved.Info)
			}
			if alreadySeen {
				continue
			}
			key := fmt.Sprintf("%d:%s", item.Size, item.SHA256)
			groups[key] = append(groups[key], item)
		}
	}
	Sort(items, q.Sort, q.Direction)
	result.Total = len(items)
	for _, files := range groups {
		if len(files) > 1 {
			Sort(files, "path", "asc")
			result.Duplicates = append(result.Duplicates, Duplicate{SHA256: files[0].SHA256, Size: files[0].Size, Files: files})
		}
	}
	sort.Slice(result.Duplicates, func(i, j int) bool {
		a, b := result.Duplicates[i], result.Duplicates[j]
		if a.Size != b.Size {
			return a.Size > b.Size
		}
		return a.SHA256 < b.SHA256
	})
	if q.Offset < 0 {
		q.Offset = 0
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	if q.Offset > len(items) {
		q.Offset = len(items)
	}
	end := q.Offset + q.Limit
	if end > len(items) {
		end = len(items)
	}
	result.Items = items[q.Offset:end]
	rows, err = s.db.Query(`SELECT data FROM library_scans WHERE owner=? AND source=? ORDER BY rowid DESC`, owner(p), q.Source)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var scan Scan
		if err = rows.Scan(&data); err != nil {
			return result, err
		}
		if json.Unmarshal([]byte(data), &scan) == nil && (within(q.Path, scan.Path) || within(scan.Path, q.Path)) {
			if _, accessErr := s.resolve(p, q.Source, scan.Path); accessErr == nil {
				result.Scan = &scan
				break
			}
		}
	}
	return result, rows.Err()
}

// Sort applies one ordering to the complete result set before pagination. Missing
// metadata always sorts last, with a stable path tie-breaker in either direction.
func Sort(items []Item, field, direction string) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := sortValue(items[i], field), sortValue(items[j], field)
		if a.missing != b.missing {
			return !a.missing
		}
		cmp := 0
		if a.number != b.number {
			if a.number < b.number {
				cmp = -1
			} else {
				cmp = 1
			}
		} else {
			cmp = strings.Compare(a.text, b.text)
		}
		if cmp == 0 {
			return items[i].Path < items[j].Path
		}
		if strings.EqualFold(direction, "desc") {
			return cmp > 0
		}
		return cmp < 0
	})
}

type value struct {
	number  float64
	text    string
	missing bool
}

func sortValue(i Item, field string) value {
	switch field {
	case "size":
		return value{number: float64(i.Size)}
	case "modified":
		return value{number: float64(i.Modified.UnixMilli())}
	case "path":
		return value{text: strings.ToLower(i.Path)}
	case "type":
		return value{text: strings.ToLower(i.Type)}
	case "extension":
		return value{text: strings.ToLower(filepath.Ext(i.Name))}
	case "width":
		return value{number: float64(i.Metadata.Width), missing: i.Metadata.Width == 0}
	case "height":
		return value{number: float64(i.Metadata.Height), missing: i.Metadata.Height == 0}
	case "duration":
		return value{number: i.Metadata.Duration, missing: i.Metadata.Duration == 0}
	case "capturedAt":
		return value{text: i.Metadata.CapturedAt, missing: i.Metadata.CapturedAt == ""}
	case "artist":
		return value{text: strings.ToLower(i.Metadata.Artist), missing: i.Metadata.Artist == ""}
	case "album":
		return value{text: strings.ToLower(i.Metadata.Album), missing: i.Metadata.Album == ""}
	case "title":
		return value{text: strings.ToLower(i.Metadata.Title), missing: i.Metadata.Title == ""}
	case "tags":
		return value{text: strings.ToLower(strings.Join(i.Tags, ",")), missing: len(i.Tags) == 0}
	default:
		return value{text: strings.ToLower(i.Name)}
	}
}

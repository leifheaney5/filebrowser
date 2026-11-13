// Package organizer runs bounded, durable, user-owned file operations. A running
// item is never replayed after a restart: its outcome must be reviewed first.
package organizer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("organizer item not found")
var ErrReview = errors.New("operation may have changed files; review source and destination before proceeding")

type Item struct {
	Source  string       `json:"source"`
	Path    string       `json:"path"`
	TrashID string       `json:"trashId,omitempty"`
	Binding string       `json:"binding,omitempty"`
	SHA256  string       `json:"expectedSha256,omitempty"`
	Keeper  *Destination `json:"keeper,omitempty"`
	Status  string       `json:"status"`
	Error   string       `json:"error,omitempty"`
}
type Destination struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Binding string `json:"binding,omitempty"`
}
type Request struct {
	Kind        string      `json:"kind"`
	Items       []Item      `json:"items"`
	Destination Destination `json:"destination"`
}
type Job struct {
	ID          string      `json:"id"`
	Owner       uint64      `json:"owner"`
	Kind        string      `json:"kind"`
	Status      string      `json:"status"`
	Total       int         `json:"total"`
	Completed   int         `json:"completed"`
	Failed      int         `json:"failed"`
	Items       []Item      `json:"items"`
	Destination Destination `json:"destination"`
	CreatedAt   time.Time   `json:"createdAt"`
	Cancelled   bool        `json:"cancelled,omitempty"`
}
type Trash struct {
	ID        string    `json:"id"`
	Owner     uint64    `json:"owner"`
	Source    string    `json:"source"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	DeletedAt time.Time `json:"deletedAt"`
	Status    string    `json:"status"`
	Binding   string    `json:"binding,omitempty"`
}
type diskState struct {
	Jobs  []*Job            `json:"jobs"`
	Trash map[string]*Trash `json:"trash"`
}
type Execute func(owner uint64, kind string, item Item, dest Destination, trash *Trash, payload string) error
type Manager struct {
	mu      sync.Mutex
	dir     string
	state   diskState
	execute Execute
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	fault   error
}

func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Open(dir string, execute Execute) (*Manager, error) {
	if err := os.MkdirAll(filepath.Join(dir, "payloads"), 0700); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, execute: execute, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), state: diskState{Trash: map[string]*Trash{}}}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil {
		if err = json.Unmarshal(data, &m.state); err != nil {
			return nil, fmt.Errorf("invalid organizer state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.state.Trash == nil {
		m.state.Trash = map[string]*Trash{}
	}
	for _, j := range m.state.Jobs {
		for i := range j.Items {
			if j.Items[i].Status == "running" {
				j.Items[i].Status = "review"
				j.Items[i].Error = ErrReview.Error()
				if t := m.state.Trash[j.Items[i].TrashID]; t != nil {
					t.Status = "review"
				}
			}
		}
		summarize(j)
	}
	if err = m.persist(); err != nil {
		return nil, err
	}
	go m.loop()
	m.signal()
	return m, nil
}
func (m *Manager) Close() { close(m.stop); <-m.done }
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) persist() error {
	data, err := json.Marshal(m.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(m.dir, "state.json"))
	}
	if err == nil {
		if d, e := os.Open(m.dir); e == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	if err != nil {
		m.fault = err
	}
	return err
}
func clone(j *Job) Job {
	out := *j
	out.Items = append([]Item(nil), j.Items...)
	for n := range out.Items {
		if out.Items[n].Keeper != nil {
			k := *out.Items[n].Keeper
			out.Items[n].Keeper = &k
		}
	}
	return out
}
func summarize(j *Job) {
	j.Completed = 0
	j.Failed = 0
	pending, running, review, cancelled := false, false, false, false
	for _, i := range j.Items {
		switch i.Status {
		case "completed":
			j.Completed++
		case "failed":
			j.Failed++
		case "pending":
			pending = true
		case "running":
			running = true
		case "review":
			review = true
		case "cancelled":
			cancelled = true
		}
	}
	switch {
	case running:
		j.Status = "running"
	case pending:
		j.Status = "queued"
	case review:
		j.Status = "review"
	case j.Cancelled || cancelled:
		j.Status = "cancelled"
	case j.Failed > 0:
		j.Status = "failed"
	default:
		j.Status = "completed"
	}
}
func (m *Manager) Submit(owner uint64, req Request) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fault != nil {
		return Job{}, m.fault
	}
	if owner == 0 {
		return Job{}, fmt.Errorf("authenticated owner required")
	}
	if req.Kind != "move" && req.Kind != "copy" && req.Kind != "trash" && req.Kind != "restore" {
		return Job{}, fmt.Errorf("invalid operation")
	}
	if len(req.Items) == 0 || len(req.Items) > 2000 {
		return Job{}, fmt.Errorf("select between 1 and 2000 items")
	}
	if len(m.state.Jobs) >= 1000 {
		return Job{}, fmt.Errorf("job history limit reached")
	}
	active := 0
	for _, j := range m.state.Jobs {
		if j.Owner == owner && (j.Status == "queued" || j.Status == "running") {
			active++
		}
	}
	if active >= 10 {
		return Job{}, fmt.Errorf("too many queued jobs")
	}
	j := &Job{ID: id(), Owner: owner, Kind: req.Kind, Items: append([]Item(nil), req.Items...), Destination: req.Destination, Total: len(req.Items), CreatedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for n := range j.Items {
		it := &j.Items[n]
		key := it.Source + "\x00" + it.Path
		if req.Kind == "restore" {
			t := m.state.Trash[it.TrashID]
			if t == nil || t.Owner != owner || t.Status != "ready" {
				return Job{}, fmt.Errorf("trash item unavailable")
			}
			it.Source = t.Source
			it.Path = t.Path
			it.Binding = t.Binding
			key = it.TrashID
		}
		if seen[key] {
			return Job{}, fmt.Errorf("duplicate item")
		}
		seen[key] = true
		if len(it.Source) > 4096 || len(it.Path) > 4096 {
			return Job{}, fmt.Errorf("path too long")
		}
		if req.Kind == "trash" {
			it.TrashID = id()
		}
		it.Status = "pending"
		it.Error = ""
	}
	summarize(j)
	m.state.Jobs = append(m.state.Jobs, j)
	if err := m.persist(); err != nil {
		return Job{}, err
	}
	m.signal()
	return clone(j), nil
}
func (m *Manager) List(owner uint64) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Job{}
	for _, j := range m.state.Jobs {
		if j.Owner == owner {
			out = append(out, clone(j))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (m *Manager) Get(owner uint64, jobID string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.state.Jobs {
		if j.ID == jobID && j.Owner == owner {
			return clone(j), nil
		}
	}
	return Job{}, ErrNotFound
}
func (m *Manager) Change(owner uint64, jobID, action string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fault != nil {
		return Job{}, m.fault
	}
	for _, j := range m.state.Jobs {
		if j.ID != jobID || j.Owner != owner {
			continue
		}
		switch action {
		case "cancel":
			j.Cancelled = true
			for n := range j.Items {
				if j.Items[n].Status == "pending" {
					j.Items[n].Status = "cancelled"
				}
			}
		case "retry":
			if j.Status == "running" || j.Status == "queued" {
				return Job{}, fmt.Errorf("job still active")
			}
			j.Cancelled = false
			for n := range j.Items {
				if j.Items[n].Status == "failed" {
					j.Items[n].Status = "pending"
					j.Items[n].Error = ""
				}
			}
		default:
			return Job{}, fmt.Errorf("unknown action")
		}
		summarize(j)
		if err := m.persist(); err != nil {
			return Job{}, err
		}
		m.signal()
		return clone(j), nil
	}
	return Job{}, ErrNotFound
}
func (m *Manager) Trash(owner uint64) []Trash {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Trash{}
	for _, t := range m.state.Trash {
		if t.Owner == owner && (t.Status == "ready" || t.Status == "review") {
			out = append(out, *t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	return out
}
func (m *Manager) loop() {
	defer close(m.done)
	for {
		select {
		case <-m.stop:
			return
		case <-m.wake:
		}
		for {
			select {
			case <-m.stop:
				return
			default:
			}
			if !m.step() {
				break
			}
		}
	}
}
func (m *Manager) step() bool {
	m.mu.Lock()
	if m.fault != nil {
		m.mu.Unlock()
		return false
	}
	var job *Job
	var n int
outer:
	for _, j := range m.state.Jobs {
		for i, it := range j.Items {
			if it.Status == "pending" {
				job = j
				n = i
				break outer
			}
		}
	}
	if job == nil {
		m.mu.Unlock()
		return false
	}
	item := job.Items[n]
	var t *Trash
	if job.Kind == "trash" {
		t = &Trash{ID: item.TrashID, Owner: job.Owner, Source: item.Source, Path: item.Path, Name: filepath.Base(item.Path), DeletedAt: time.Now().UTC(), Status: "preparing", Binding: item.Binding}
		m.state.Trash[t.ID] = t
	}
	if job.Kind == "restore" {
		if old := m.state.Trash[item.TrashID]; old != nil && old.Owner == job.Owner && old.Status == "ready" {
			v := *old
			t = &v
		}
	}
	job.Items[n].Status = "running"
	summarize(job)
	if err := m.persist(); err != nil {
		m.mu.Unlock()
		return false
	}
	owner, kind, dest := job.Owner, job.Kind, job.Destination
	payload := filepath.Join(m.dir, "payloads", item.TrashID)
	m.mu.Unlock()
	var err error
	if kind == "restore" && t == nil {
		err = fmt.Errorf("trash item unavailable")
	} else {
		err = m.execute(owner, kind, item, dest, t, payload)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	it := &job.Items[n]
	if err == nil {
		it.Status = "completed"
		if kind == "trash" {
			m.state.Trash[item.TrashID].Status = "ready"
		}
		if kind == "restore" {
			delete(m.state.Trash, item.TrashID)
		}
	} else {
		it.Status = "failed"
		if errors.Is(err, ErrReview) {
			it.Status = "review"
		}
		it.Error = err.Error()
		if kind == "trash" {
			if it.Status == "review" {
				m.state.Trash[item.TrashID].Status = "review"
			} else {
				delete(m.state.Trash, item.TrashID)
			}
		}
	}
	summarize(job)
	_ = m.persist()
	return m.fault == nil
}

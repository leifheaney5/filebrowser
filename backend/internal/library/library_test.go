package library

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, Principal, *atomic.Bool, Resolver) {
	t.Helper()
	root := t.TempDir()
	p := Principal{UserID: 1, SourceRoot: root, Scope: "/", View: true, Download: true}
	allowed := &atomic.Bool{}
	allowed.Store(true)
	resolve := func(p Principal, source, name string) (Resolved, error) {
		if !allowed.Load() || p.UserID != 1 || !p.View || !p.Download || source != "test" || strings.Contains(name, "private") {
			return Resolved{}, ErrDenied
		}
		name = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
		info, err := os.Lstat(name)
		if err != nil {
			return Resolved{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return Resolved{}, ErrDenied
		}
		return Resolved{Absolute: name, Info: info}, nil
	}
	return root, p, allowed, resolve
}
func write(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func openService(t *testing.T, db string, resolve Resolver) *Service {
	t.Helper()
	s, err := Open(db, resolve)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func waitScan(t *testing.T, s *Service, p Principal, source string) Result {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r, err := s.List(p, Query{Source: source, Path: "/", Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if r.Scan != nil && (r.Scan.Status == "completed" || r.Scan.Status == "failed") {
			if r.Scan.Status == "failed" {
				t.Fatalf("scan failed: %+v", r.Scan)
			}
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not complete")
	return Result{}
}

func TestCrossFolderFullSHA256HardlinksAndAccessRevalidation(t *testing.T) {
	root, p, allowed, resolve := fixture(t)
	db := filepath.Join(t.TempDir(), "library.sqlite")
	s := openService(t, db, resolve)
	defer s.Close()
	content := bytes.Repeat([]byte("a"), 64*1024)
	write(t, filepath.Join(root, "one", "first.bin"), content)
	write(t, filepath.Join(root, "two", "different-name.bin"), content)
	different := bytes.Clone(content)
	different[len(different)-1] = 'z'
	write(t, filepath.Join(root, "two", "same-headers-different-suffix.bin"), different)
	if err := os.Link(filepath.Join(root, "one", "first.bin"), filepath.Join(root, "one", "hardlink.bin")); err != nil {
		t.Logf("hardlink unsupported: %v", err)
	}
	write(t, filepath.Join(root, "private", "secret.bin"), content)
	write(t, filepath.Join(root, ".trash", "deleted.bin"), content)
	if _, err := s.Start(p, "test", "/"); err != nil {
		t.Fatal(err)
	}
	result := waitScan(t, s, p, "test")
	if result.Total != 3 {
		t.Fatalf("want three physical files, got %+v", result)
	}
	if len(result.Duplicates) != 1 || len(result.Duplicates[0].Files) != 2 {
		t.Fatalf("want exactly two full-content duplicates: %+v", result.Duplicates)
	}
	if result.Duplicates[0].Files[0].SHA256 == "" || len(result.Duplicates[0].SHA256) != 64 {
		t.Fatal("missing complete SHA256")
	}
	for _, item := range result.Duplicates[0].Files {
		if strings.Contains(item.Path, "suffix") {
			t.Fatal("partial checksums incorrectly matched differing suffix")
		}
	}
	childResult, err := s.List(p, Query{Source: "test", Path: "/one"})
	if err != nil || childResult.Scan == nil || childResult.Scan.Path != "/" {
		t.Fatalf("covering root scan missing in child location: %+v %v", childResult.Scan, err)
	}
	// A cached index is never authority: revocation blocks both listing and new work.
	allowed.Store(false)
	if _, err := s.List(p, Query{Source: "test", Path: "/"}); err == nil {
		t.Fatal("cached contents leaked after revocation")
	}
	if _, err := s.Start(p, "test", "/"); err == nil {
		t.Fatal("scan accepted after revocation")
	}
	allowed.Store(true)
	other := p
	other.UserID = 2
	if _, err := s.List(other, Query{Source: "test", Path: "/"}); err == nil {
		t.Fatal("another owner obtained cached contents")
	}
}

func TestHardlinksAcrossIndependentScansAreNotDuplicates(t *testing.T) {
	root, p, _, resolve := fixture(t)
	write(t, filepath.Join(root, "a", "first.bin"), []byte("shared physical bytes"))
	if err := os.MkdirAll(filepath.Join(root, "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a", "first.bin"), filepath.Join(root, "b", "second.bin")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	s := openService(t, filepath.Join(t.TempDir(), "library.sqlite"), resolve)
	defer s.Close()
	if _, err := s.Start(p, "test", "/a"); err != nil {
		t.Fatal(err)
	}
	waitScan(t, s, p, "test")
	if _, err := s.Start(p, "test", "/b"); err != nil {
		t.Fatal(err)
	}
	result := waitScan(t, s, p, "test")
	if result.Total != 2 || len(result.Duplicates) != 0 {
		t.Fatalf("hardlinks incorrectly represent reclaimable duplicates: %+v", result)
	}
}

func TestMetadataPersonalTagsPersistenceAndStaleFile(t *testing.T) {
	root, p, _, resolve := fixture(t)
	db := filepath.Join(t.TempDir(), "library.sqlite")
	imagePath := filepath.Join(root, "photo.png")
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 37, 19))); err != nil {
		t.Fatal(err)
	}
	write(t, imagePath, pngData.Bytes())
	s := openService(t, db, resolve)
	if _, err := s.Start(p, "test", "/"); err != nil {
		t.Fatal(err)
	}
	result := waitScan(t, s, p, "test")
	if len(result.Items) != 1 || result.Items[0].Metadata.Width != 37 || result.Items[0].Metadata.Height != 19 {
		t.Fatalf("dimensions not indexed: %+v", result.Items)
	}
	if _, err := s.SetTags(p, "test", "/photo.png", []string{"Family", " family ", "Trip"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openService(t, db, resolve)
	defer s.Close()
	result, err := s.List(p, Query{Source: "test", Path: "/", Search: "family"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || strings.Join(result.Items[0].Tags, ",") != "Family,Trip" {
		t.Fatalf("tags did not persist: %+v", result)
	}
	write(t, imagePath, []byte("replacement"))
	result, err = s.List(p, Query{Source: "test", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Duplicates) != 0 {
		t.Fatal("stale hash was returned for changed file")
	}
}

func TestSortEntireResultBeforePaginationAndMissingLast(t *testing.T) {
	root, p, _, resolve := fixture(t)
	s := openService(t, filepath.Join(t.TempDir(), "library.sqlite"), resolve)
	defer s.Close()
	write(t, filepath.Join(root, "a.bin"), bytes.Repeat([]byte{'a'}, 3))
	write(t, filepath.Join(root, "b.bin"), bytes.Repeat([]byte{'b'}, 1))
	write(t, filepath.Join(root, "c.bin"), bytes.Repeat([]byte{'c'}, 2))
	if _, err := s.Start(p, "test", "/"); err != nil {
		t.Fatal(err)
	}
	waitScan(t, s, p, "test")
	result, err := s.List(p, Query{Source: "test", Path: "/", Sort: "size", Direction: "asc", Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].Name != "c.bin" {
		t.Fatalf("sorting occurred after paging: %+v", result)
	}
	items := []Item{{Path: "/missing", Metadata: Metadata{}}, {Path: "/large", Metadata: Metadata{Width: 200}}, {Path: "/small", Metadata: Metadata{Width: 20}}}
	Sort(items, "width", "desc")
	if items[0].Path != "/large" || items[2].Path != "/missing" {
		t.Fatal("descending missing-value ordering incorrect")
	}
	Sort(items, "width", "asc")
	if items[0].Path != "/small" || items[2].Path != "/missing" {
		t.Fatal("ascending missing-value ordering incorrect")
	}
}

func TestResumeRunningScanWithoutBrowserRequest(t *testing.T) {
	root, p, _, resolve := fixture(t)
	db := filepath.Join(t.TempDir(), "library.sqlite")
	write(t, filepath.Join(root, "first.txt"), []byte("first"))
	s := openService(t, db, resolve)
	scan, err := s.Start(p, "test", "/")
	if err != nil {
		t.Fatal(err)
	}
	waitScan(t, s, p, "test")
	// Simulate a server process exiting with its durable scan record still running.
	scan.Status = "running"
	if err = s.saveScan(scan); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "second.txt"), []byte("second"))
	s = openService(t, db, resolve)
	defer s.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var raw string
		if err = s.db.QueryRow(`SELECT data FROM library_scans WHERE id=?`, scan.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var resumed Scan
		if err = json.Unmarshal([]byte(raw), &resumed); err != nil {
			t.Fatal(err)
		}
		if resumed.Status == "completed" && resumed.Processed == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan was not resumed at service startup")
}

func TestAudioTagsExtraction(t *testing.T) {
	var frames bytes.Buffer
	for _, field := range []struct{ name, value string }{{"TIT2", "Original song"}, {"TPE1", "Example artist"}, {"TALB", "Example album"}} {
		frames.WriteString(field.name)
		_ = binary.Write(&frames, binary.BigEndian, uint32(len(field.value)+1))
		frames.Write([]byte{0, 0, 0})
		frames.WriteString(field.value)
	}
	size := frames.Len()
	header := []byte{'I', 'D', '3', 3, 0, 0, byte(size>>21) & 127, byte(size>>14) & 127, byte(size>>7) & 127, byte(size) & 127}
	filename := filepath.Join(t.TempDir(), "track.mp3")
	write(t, filename, append(header, frames.Bytes()...))
	m := extractMetadata(t.Context(), filename, "audio")
	if m.Title != "Original song" || m.Artist != "Example artist" || m.Album != "Example album" {
		t.Fatalf("audio tags missing: %+v", m)
	}
}

func TestProbeLocalAudioDuration(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	var wave bytes.Buffer
	wave.WriteString("RIFF")
	_ = binary.Write(&wave, binary.LittleEndian, uint32(36+16000))
	wave.WriteString("WAVEfmt ")
	_ = binary.Write(&wave, binary.LittleEndian, uint32(16))
	for _, v := range []uint16{1, 1} {
		_ = binary.Write(&wave, binary.LittleEndian, v)
	}
	_ = binary.Write(&wave, binary.LittleEndian, uint32(8000))
	_ = binary.Write(&wave, binary.LittleEndian, uint32(16000))
	_ = binary.Write(&wave, binary.LittleEndian, uint16(2))
	_ = binary.Write(&wave, binary.LittleEndian, uint16(16))
	wave.WriteString("data")
	_ = binary.Write(&wave, binary.LittleEndian, uint32(16000))
	wave.Write(make([]byte, 16000))
	filename := filepath.Join(t.TempDir(), "one-second.wav")
	write(t, filename, wave.Bytes())
	m := extractMetadata(t.Context(), filename, "audio")
	if m.Duration < 0.99 || m.Duration > 1.01 {
		t.Fatalf("duration metadata absent/incorrect: %+v", m)
	}
}

func TestPhotoCaptureDateExtraction(t *testing.T) {
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 4, 3)), nil); err != nil {
		t.Fatal(err)
	}
	var tiff bytes.Buffer
	tiff.WriteString("II")
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x132))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(2))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(20))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(26))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0))
	tiff.WriteString("2023:10:31 12:34:56\x00")
	exifPayload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var encoded bytes.Buffer
	encoded.Write(photo.Bytes()[:2])
	encoded.Write([]byte{0xff, 0xe1})
	_ = binary.Write(&encoded, binary.BigEndian, uint16(len(exifPayload)+2))
	encoded.Write(exifPayload)
	encoded.Write(photo.Bytes()[2:])
	filename := filepath.Join(t.TempDir(), "capture.jpg")
	write(t, filename, encoded.Bytes())
	m := extractMetadata(t.Context(), filename, "image")
	if !strings.HasPrefix(m.CapturedAt, "2023-10-31T12:34:56") || m.Width != 4 || m.Height != 3 {
		t.Fatalf("photo metadata missing: %+v", m)
	}
}

func BenchmarkLibraryThousandFiles(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "query"} {
		b.Run(mode, func(b *testing.B) {
			root := b.TempDir()
			p := Principal{UserID: 1, SourceRoot: root, Scope: "/", View: true, Download: true}
			resolver := func(p Principal, source, name string) (Resolved, error) {
				filename := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(name, "/")))
				info, err := os.Lstat(filename)
				return Resolved{Absolute: filename, Info: info}, err
			}
			for i := 0; i < 1000; i++ {
				filename := filepath.Join(root, fmt.Sprintf("folder-%02d", i/100), fmt.Sprintf("file-%04d.bin", i))
				if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(filename, bytes.Repeat([]byte{byte(i % 251)}, 1024), 0600); err != nil {
					b.Fatal(err)
				}
			}
			s, err := Open(filepath.Join(b.TempDir(), "library.sqlite"), resolver)
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			scan := Scan{ID: "initial", Source: "test", Path: "/", Principal: p, Started: time.Now().UTC()}
			if mode != "cold" {
				s.scan(scan)
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if mode == "query" {
					result, err := s.List(p, Query{Source: "test", Path: "/", Sort: "size", Limit: 100})
					if err != nil || result.Total != 1000 {
						b.Fatalf("query: %d %v", result.Total, err)
					}
				} else {
					scan.ID = fmt.Sprintf("bench-%d", n)
					s.scan(scan)
				}
			}
			b.StopTimer()
		})
	}
}

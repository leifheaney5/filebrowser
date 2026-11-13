package web

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateVerificationIncludesLastByte(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("a"), 65536)
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	if err := os.WriteFile(a, data, 0600); err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = 'b'
	if err := os.WriteFile(b, data, 0600); err != nil {
		t.Fatal(err)
	}
	ai, _ := os.Stat(a)
	bi, _ := os.Stat(b)
	ha, err := computeMiddleChecksum(dir, a, ai.Size(), ai.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	hb, err := computeMiddleChecksum(dir, b, bi.Size(), bi.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Fatal("different final bytes were reported as duplicates")
	}
	if len(ha) != 64 {
		t.Fatalf("expected SHA-256, got %q", ha)
	}
}

func TestDuplicateVerificationRejectsStaleIndexAndEscape(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "file")
	if err := os.WriteFile(p, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if _, err := computeMiddleChecksum(dir, p, 3, info.ModTime().Add(-time.Second)); err == nil {
		t.Fatal("stale index metadata accepted")
	}
	if _, err := computeMiddleChecksum(filepath.Join(dir, "subdir"), p, 3, info.ModTime()); err == nil {
		t.Fatal("path outside source accepted")
	}
}

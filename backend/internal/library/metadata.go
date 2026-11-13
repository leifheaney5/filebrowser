package library

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/webp"
)

func fileType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff", ".tif", ".heic", ".heif":
		return "image"
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".mpeg", ".mpg":
		return "video"
	case ".mp3", ".m4a", ".flac", ".ogg", ".wav", ".aac", ".opus", ".aiff":
		return "audio"
	case ".txt", ".md", ".json", ".csv", ".log", ".yaml", ".yml", ".xml":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	default:
		return "file"
	}
}

// Header extraction uses bounded readers. ffprobe runs sequentially in the scan
// worker, with a deadline and bounded output. Only an already-open local input
// is passed over stdin, and the file/network protocols are disabled so crafted
// playlists or media references cannot make ffprobe read other private files.
func extractMetadata(ctx context.Context, filename, kind string) Metadata {
	f, err := os.Open(filename)
	if err != nil {
		return Metadata{}
	}
	defer f.Close()
	return metadataFromFile(ctx, f, kind)
}
func metadataFromFile(ctx context.Context, f *os.File, kind string) Metadata {
	var m Metadata
	if ctx.Err() != nil {
		return m
	}
	_, _ = f.Seek(0, io.SeekStart)
	if kind == "image" {
		if cfg, _, err := image.DecodeConfig(io.LimitReader(f, 8<<20)); err == nil {
			m.Width = cfg.Width
			m.Height = cfg.Height
		}
		_, _ = f.Seek(0, io.SeekStart)
		if data, err := exif.Decode(io.LimitReader(f, 8<<20)); err == nil {
			if captured, err := data.DateTime(); err == nil {
				// Cameras commonly store a wall-clock capture time without an
				// offset. Do not invent an offset from the server's timezone.
				if zone, zoneErr := data.TimeZone(); zoneErr == nil && zone != nil {
					m.CapturedAt = captured.UTC().Format(time.RFC3339Nano)
				} else {
					m.CapturedAt = captured.Format("2006-01-02T15:04:05")
				}
			}
		}
	}
	if kind == "audio" {
		if info, err := f.Stat(); err == nil {
			length := info.Size()
			if length > 32<<20 {
				length = 32 << 20
			}
			if data, err := tag.ReadFrom(io.NewSectionReader(f, 0, length)); err == nil {
				m.Title = data.Title()
				m.Artist = data.Artist()
				m.Album = data.Album()
			}
		}
	}
	if kind == "audio" || kind == "video" || (kind == "image" && m.Width == 0) {
		_, _ = f.Seek(0, io.SeekStart)
		probeMetadata(ctx, f, &m)
	}
	return m
}

type boundedOutput struct {
	data  []byte
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		return 0, errors.New("metadata output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func probeMetadata(ctx context.Context, f *os.File, m *Metadata) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "fd", "-probesize", "5000000", "-analyzeduration", "2000000", "-fd", "0", "-i", "fd:", "-show_entries", "format=duration:format_tags=creation_time,artist,album,title:stream=width,height", "-of", "json")
	cmd.Stdin = f
	output := &boundedOutput{limit: 1 << 20}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil {
		return
	}
	var data struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if json.Unmarshal(output.data, &data) != nil {
		return
	}
	if duration, err := strconv.ParseFloat(data.Format.Duration, 64); err == nil && duration > 0 {
		m.Duration = duration
	}
	for _, stream := range data.Streams {
		if stream.Width > 0 && stream.Height > 0 {
			m.Width = stream.Width
			m.Height = stream.Height
			break
		}
	}
	for key, val := range data.Format.Tags {
		switch strings.ToLower(key) {
		case "artist":
			if m.Artist == "" {
				m.Artist = val
			}
		case "album":
			if m.Album == "" {
				m.Album = val
			}
		case "title":
			if m.Title == "" {
				m.Title = val
			}
		case "creation_time":
			if m.CapturedAt == "" {
				if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
					m.CapturedAt = t.UTC().Format(time.RFC3339Nano)
				}
			}
		}
	}
}

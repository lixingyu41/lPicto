package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Metadata struct {
	MimeType        string
	Width           *int
	Height          *int
	Duration        *float64
	TakenAt         *int64
	VideoCreatedAt  *int64
	TimelineAt      int64
	BrowserPlayable bool
	RawJSON         string
	Err             error
}

type Extractor struct {
	CommandTimeout time.Duration
}

func NewExtractor() Extractor {
	return Extractor{CommandTimeout: 45 * time.Second}
}

func (e Extractor) Extract(ctx context.Context, path string, detection Detection, mtime, importedAt int64) Metadata {
	if detection.MediaType == "video" {
		return e.extractVideo(ctx, path, detection, mtime, importedAt)
	}
	return e.extractImage(ctx, path, detection, mtime, importedAt)
}

func TimelineAt(takenAt, videoCreatedAt *int64, mtime, importedAt int64) int64 {
	if takenAt != nil && *takenAt > 0 {
		return *takenAt
	}
	if videoCreatedAt != nil && *videoCreatedAt > 0 {
		return *videoCreatedAt
	}
	if mtime > 0 {
		return mtime
	}
	return importedAt
}

func (e Extractor) extractImage(ctx context.Context, path string, detection Detection, mtime, importedAt int64) Metadata {
	meta := Metadata{MimeType: detection.MimeType}
	data, err := e.run(ctx, "exiftool", "-json", "-n", "-MIMEType", "-ImageWidth", "-ImageHeight", "-DateTimeOriginal", "-CreateDate", path)
	if err != nil {
		meta.TimelineAt = TimelineAt(nil, nil, mtime, importedAt)
		meta.Err = err
		return meta
	}
	meta.RawJSON = string(data)
	var docs []map[string]any
	if err := json.Unmarshal(data, &docs); err != nil || len(docs) == 0 {
		meta.TimelineAt = TimelineAt(nil, nil, mtime, importedAt)
		meta.Err = fmt.Errorf("parse exiftool json: %w", err)
		return meta
	}
	doc := docs[0]
	if mimeValue, ok := stringValue(doc["MIMEType"]); ok {
		meta.MimeType = mimeValue
	}
	meta.Width = intPtrValue(doc["ImageWidth"])
	meta.Height = intPtrValue(doc["ImageHeight"])
	taken := firstUnixTime(doc["DateTimeOriginal"], doc["CreateDate"])
	meta.TakenAt = taken
	meta.TimelineAt = TimelineAt(taken, nil, mtime, importedAt)
	return meta
}

func (e Extractor) extractVideo(ctx context.Context, path string, detection Detection, mtime, importedAt int64) Metadata {
	meta := Metadata{MimeType: detection.MimeType}
	data, err := e.run(ctx, "ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	if err != nil {
		meta.TimelineAt = TimelineAt(nil, nil, mtime, importedAt)
		meta.Err = err
		return meta
	}
	meta.RawJSON = string(data)
	var probe ffprobeResult
	if err := json.Unmarshal(data, &probe); err != nil {
		meta.TimelineAt = TimelineAt(nil, nil, mtime, importedAt)
		meta.Err = fmt.Errorf("parse ffprobe json: %w", err)
		return meta
	}
	var videoCodec string
	var audioCodec string
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if meta.Width == nil && stream.Width > 0 {
				meta.Width = &stream.Width
			}
			if meta.Height == nil && stream.Height > 0 {
				meta.Height = &stream.Height
			}
			if videoCodec == "" {
				videoCodec = strings.ToLower(stream.CodecName)
			}
			if created := tagUnixTime(stream.Tags); created != nil && meta.VideoCreatedAt == nil {
				meta.VideoCreatedAt = created
			}
		case "audio":
			if audioCodec == "" {
				audioCodec = strings.ToLower(stream.CodecName)
			}
		}
	}
	if probe.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			meta.Duration = &duration
		}
	}
	if created := tagUnixTime(probe.Format.Tags); created != nil {
		meta.VideoCreatedAt = created
	}
	meta.BrowserPlayable = BrowserVideoPlayable(detection.Ext, videoCodec, audioCodec)
	meta.TimelineAt = TimelineAt(nil, meta.VideoCreatedAt, mtime, importedAt)
	return meta
}

func BrowserVideoPlayable(ext, videoCodec, audioCodec string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	videoCodec = strings.ToLower(videoCodec)
	audioCodec = strings.ToLower(audioCodec)
	audioOKMP4 := audioCodec == "" || audioCodec == "aac" || audioCodec == "mp3"
	audioOKWebM := audioCodec == "" || audioCodec == "opus" || audioCodec == "vorbis"
	switch ext {
	case "mp4", "m4v":
		return (videoCodec == "h264" || videoCodec == "avc1") && audioOKMP4
	case "webm":
		return (videoCodec == "vp8" || videoCodec == "vp9" || videoCodec == "av1") && audioOKWebM
	default:
		return false
	}
}

func (e Extractor) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, e.CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if cmdCtx.Err() != nil {
		return nil, cmdCtx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

type ffprobeResult struct {
	Streams []struct {
		CodecType string            `json:"codec_type"`
		CodecName string            `json:"codec_name"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
		Tags      map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

func stringValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			return typed, true
		}
	case fmt.Stringer:
		return typed.String(), true
	}
	return "", false
}

func intPtrValue(value any) *int {
	switch typed := value.(type) {
	case float64:
		v := int(typed)
		return &v
	case int:
		v := typed
		return &v
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil {
			return &parsed
		}
	}
	return nil
}

func firstUnixTime(values ...any) *int64 {
	for _, value := range values {
		if parsed := unixTime(value); parsed != nil {
			return parsed
		}
	}
	return nil
}

func tagUnixTime(tags map[string]string) *int64 {
	if len(tags) == 0 {
		return nil
	}
	keys := []string{"creation_time", "CreationTime", "com.apple.quicktime.creationdate", "date"}
	for _, key := range keys {
		if value, ok := tags[key]; ok {
			if parsed := unixTime(value); parsed != nil {
				return parsed
			}
		}
	}
	return nil
}

func unixTime(value any) *int64 {
	text, ok := stringValue(value)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006:01:02 15:04:05",
		"2006:01:02 15:04:05-07:00",
		"2006:01:02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z0700",
		"2006-01-02",
		"2006/01/02",
		"2006.01.02",
		"2006-01",
		"2006/01",
		"2006.01",
		"2006",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			unix := parsed.Unix()
			return &unix
		}
	}
	return nil
}

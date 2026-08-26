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
	HasVideo        bool
	HasAudio        bool
	Width           *int
	Height          *int
	Duration        *float64
	TakenAt         *int64
	VideoCreatedAt  *int64
	TimelineAt      int64
	BrowserPlayable bool
	FPS             *float64
	VideoCodec      *string
	AudioCodec      *string
	Container       *string
	VideoBitrate    *int64
	AudioBitrate    *int64
	OverallBitrate  *int64
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
	if detection.MediaType == "video" || detection.MediaType == "audio" {
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
	data, err := e.run(ctx, "exiftool", "-json", "-n", "-MIMEType", "-ImageWidth", "-ImageHeight", "-Orientation", "-DateTimeOriginal", "-CreateDate", "-Title", "-ObjectName", "-XPTitle", "-Headline", path)
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
	meta.Width, meta.Height = displayImageDimensions(meta.Width, meta.Height, intPtrValue(doc["Orientation"]))
	taken := firstUnixTime(doc["DateTimeOriginal"], doc["CreateDate"])
	meta.TakenAt = taken
	meta.TimelineAt = TimelineAt(taken, nil, mtime, importedAt)
	return meta
}

func displayImageDimensions(width, height, orientation *int) (*int, *int) {
	if width == nil || height == nil || orientation == nil {
		return width, height
	}
	switch *orientation {
	case 5, 6, 7, 8:
		return height, width
	default:
		return width, height
	}
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
			meta.HasVideo = true
			if meta.Width == nil && stream.Width > 0 {
				meta.Width = &stream.Width
			}
			if meta.Height == nil && stream.Height > 0 {
				meta.Height = &stream.Height
			}
			if videoCodec == "" {
				videoCodec = strings.ToLower(stream.CodecName)
				meta.VideoCodec = stringPtrValue(codecDisplayName(stream.CodecName, stream.Profile))
				meta.VideoBitrate = positiveInt64Value(stream.BitRate)
				meta.FPS = frameRateValue(stream.AvgFrameRate)
				if meta.FPS == nil {
					meta.FPS = frameRateValue(stream.RFrameRate)
				}
			}
			if created := tagUnixTime(stream.Tags); created != nil && meta.VideoCreatedAt == nil {
				meta.VideoCreatedAt = created
			}
		case "audio":
			meta.HasAudio = true
			if audioCodec == "" {
				audioCodec = strings.ToLower(stream.CodecName)
				meta.AudioCodec = stringPtrValue(codecDisplayName(stream.CodecName, stream.Profile))
				meta.AudioBitrate = positiveInt64Value(stream.BitRate)
			}
		}
	}
	meta.Container = stringPtrValue(probe.Format.FormatName)
	meta.OverallBitrate = positiveInt64Value(probe.Format.BitRate)
	if probe.Format.Duration != "" {
		if duration, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
			meta.Duration = &duration
		}
	}
	if created := tagUnixTime(probe.Format.Tags); created != nil {
		meta.VideoCreatedAt = created
	}
	if detection.MediaType == "audio" || (!meta.HasVideo && meta.HasAudio) {
		meta.BrowserPlayable = BrowserAudioPlayable(detection.Ext, audioCodec)
	} else {
		meta.BrowserPlayable = BrowserVideoPlayable(detection.Ext, videoCodec, audioCodec)
	}
	meta.TimelineAt = TimelineAt(nil, meta.VideoCreatedAt, mtime, importedAt)
	return meta
}

func StreamKindsFromMetadataJSON(raw string) (hasVideo bool, hasAudio bool, audioCodec string) {
	if strings.TrimSpace(raw) == "" {
		return false, false, ""
	}
	var probe ffprobeResult
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return false, false, ""
	}
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			hasVideo = true
		case "audio":
			hasAudio = true
			if audioCodec == "" {
				audioCodec = strings.ToLower(strings.TrimSpace(stream.CodecName))
			}
		}
	}
	return hasVideo, hasAudio, audioCodec
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
		CodecType    string            `json:"codec_type"`
		CodecName    string            `json:"codec_name"`
		Profile      string            `json:"profile"`
		BitRate      string            `json:"bit_rate"`
		AvgFrameRate string            `json:"avg_frame_rate"`
		RFrameRate   string            `json:"r_frame_rate"`
		Width        int               `json:"width"`
		Height       int               `json:"height"`
		Tags         map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Duration   string            `json:"duration"`
		FormatName string            `json:"format_name"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
}

func codecDisplayName(codec, profile string) string {
	codec = strings.TrimSpace(codec)
	profile = strings.TrimSpace(profile)
	if codec == "" {
		return ""
	}
	if profile == "" || strings.EqualFold(codec, profile) {
		return codec
	}
	return codec + " " + profile
}

func stringPtrValue(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func positiveInt64Value(value string) *int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func frameRateValue(value string) *float64 {
	value = strings.TrimSpace(value)
	if value == "" || value == "0/0" {
		return nil
	}
	if !strings.Contains(value, "/") {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil && parsed > 0 {
			return &parsed
		}
		return nil
	}
	parts := strings.SplitN(value, "/", 2)
	numerator, firstErr := strconv.ParseFloat(parts[0], 64)
	denominator, secondErr := strconv.ParseFloat(parts[1], 64)
	if firstErr != nil || secondErr != nil || numerator <= 0 || denominator <= 0 {
		return nil
	}
	result := numerator / denominator
	return &result
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
	layouts := []struct {
		layout string
		local  bool
	}{
		{time.RFC3339Nano, false},
		{time.RFC3339, false},
		{"2006:01:02 15:04:05", true},
		{"2006:01:02 15:04:05-07:00", false},
		{"2006:01:02 15:04:05Z07:00", false},
		{"2006-01-02 15:04:05", true},
		{"2006-01-02T15:04:05", true},
		{"2006-01-02T15:04:05Z0700", false},
		{"2006-01-02", true},
		{"2006/01/02", true},
		{"2006.01.02", true},
		{"2006-01", true},
		{"2006/01", true},
		{"2006.01", true},
		{"2006", true},
	}
	for _, item := range layouts {
		var parsed time.Time
		var err error
		if item.local {
			parsed, err = time.ParseInLocation(item.layout, text, time.Local)
		} else {
			parsed, err = time.Parse(item.layout, text)
		}
		if err == nil {
			unix := parsed.Unix()
			return &unix
		}
	}
	return nil
}

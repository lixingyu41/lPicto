package api

import (
	"math"
	"testing"

	"lpicto/backend/internal/model"
)

func TestParseAssetMediaDetails(t *testing.T) {
	raw := `{
  "streams": [
    {"codec_type":"video","codec_name":"h264","profile":"High","bit_rate":"4800000","avg_frame_rate":"30000/1001"},
    {"codec_type":"audio","codec_name":"aac","profile":"LC","bit_rate":"192000"}
  ],
  "format": {"format_name":"mov,mp4,m4a,3gp,3g2,mj2","bit_rate":"4992000"}
}`
	details := parseAssetMediaDetails(&raw)
	if details.FPS == nil || math.Abs(*details.FPS-29.97002997) > 0.0001 {
		t.Fatalf("unexpected fps: %v", details.FPS)
	}
	if details.VideoCodec == nil || *details.VideoCodec != "h264 (High)" {
		t.Fatalf("unexpected video codec: %v", details.VideoCodec)
	}
	if details.AudioCodec == nil || *details.AudioCodec != "aac (LC)" {
		t.Fatalf("unexpected audio codec: %v", details.AudioCodec)
	}
	if details.VideoBitrate == nil || *details.VideoBitrate != 4800000 {
		t.Fatalf("unexpected video bitrate: %v", details.VideoBitrate)
	}
	if details.AudioBitrate == nil || *details.AudioBitrate != 192000 {
		t.Fatalf("unexpected audio bitrate: %v", details.AudioBitrate)
	}
	if details.OverallBitrate == nil || *details.OverallBitrate != 4992000 {
		t.Fatalf("unexpected overall bitrate: %v", details.OverallBitrate)
	}
}

func TestParseAssetMediaDetailsDerivesVideoBitrateWithoutAudio(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","codec_name":"hevc","r_frame_rate":"25/1"}],"format":{"format_name":"matroska,webm","bit_rate":"8000000"}}`
	details := parseAssetMediaDetails(&raw)
	if details.VideoBitrate == nil || *details.VideoBitrate != 8000000 {
		t.Fatalf("unexpected derived video bitrate: %v", details.VideoBitrate)
	}
}

func TestParseAssetMediaDetailsRejectsBrokenStreamBitrates(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","codec_name":"av1","bit_rate":"3847"},{"codec_type":"audio","codec_name":"aac","bit_rate":"660"}],"format":{"bit_rate":"798377"}}`
	details := parseAssetMediaDetails(&raw)
	if details.VideoBitrate != nil || details.AudioBitrate != nil {
		t.Fatalf("expected broken stream bitrates to be discarded: video=%v audio=%v", details.VideoBitrate, details.AudioBitrate)
	}
	if details.OverallBitrate == nil || *details.OverallBitrate != 798377 {
		t.Fatalf("unexpected overall bitrate: %v", details.OverallBitrate)
	}
}

func TestAssetDisplayTitlePriority(t *testing.T) {
	metadata := `{"format":{"tags":{"title":"  Embedded   title  "}}}`
	nfo := `{"fields":{"标题":"NFO title"},"groups":[{"items":[{"key":"title","value":"NFO title"}]}]}`
	asset := model.Asset{Filename: "fallback.mp4", MetadataJSON: &metadata, NFOJSON: &nfo}
	if got := assetDisplayTitle(asset); got != "Embedded title" {
		t.Fatalf("display title = %q", got)
	}
	asset.MetadataJSON = nil
	if got := assetDisplayTitle(asset); got != "NFO title" {
		t.Fatalf("NFO display title = %q", got)
	}
	asset.NFOJSON = nil
	if got := assetDisplayTitle(asset); got != "fallback.mp4" {
		t.Fatalf("filename display title = %q", got)
	}
}

func TestAssetDisplayTitleFromImageMetadata(t *testing.T) {
	metadata := `[{"SourceFile":"photo.jpg","Title":"Portrait title"}]`
	asset := model.Asset{Filename: "photo.jpg", MetadataJSON: &metadata}
	if got := assetDisplayTitle(asset); got != "Portrait title" {
		t.Fatalf("image display title = %q", got)
	}
}

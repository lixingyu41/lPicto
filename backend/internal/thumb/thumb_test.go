package thumb

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"lpicto/backend/internal/model"
)

func TestStoryboardTimingUsesVideoStreamInsteadOfLongerAudio(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","start_time":"0.000000","duration":"224.033333"},{"codec_type":"audio","start_time":"0.000000","duration":"584.748118"}]}`
	timing := storyboardTiming(&raw, 584.747778)
	if math.Abs(timing.Start) > 0.0001 || math.Abs(timing.Duration-224.033333) > 0.0001 {
		t.Fatalf("timing = %#v", timing)
	}
	filter := storyboardFilter(model.Asset{MetadataJSON: &raw}, 584.747778, 15)
	if !strings.Contains(filter, "stop_duration=360.714445") {
		t.Fatalf("filter does not pad the audio-only tail: %s", filter)
	}
}

func TestStoryboardTimingPreservesDelayedVideoPosition(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","start_time":"100.000000","duration":"172.433000"},{"codec_type":"audio","start_time":"0.000000","duration":"272.533000"}]}`
	filter := storyboardFilter(model.Asset{MetadataJSON: &raw}, 272.533, 15)
	if !strings.Contains(filter, "start_duration=100.000000") || !strings.Contains(filter, "stop_duration=0.100000") {
		t.Fatalf("filter does not preserve the video timeline: %s", filter)
	}
}

func TestStoryboardVAAPIFilterScalesBeforeDownloadingFrames(t *testing.T) {
	filter := storyboardVAAPIFilter(model.Asset{}, 60, 3.75)
	wantOrder := []string{
		"scale_vaapi=w=160:h=90:force_original_aspect_ratio=decrease",
		"hwdownload",
		"format=nv12",
		"setpts=PTS-STARTPTS",
		"fps=1/3.750000",
		"pad=160:90:(ow-iw)/2:(oh-ih)/2:black",
		"tile=4x4:nb_frames=16",
	}
	position := -1
	for _, want := range wantOrder {
		found := strings.Index(filter[position+1:], want)
		if found < 0 {
			t.Fatalf("filter = %q, missing ordered element %q", filter, want)
		}
		position += found + 1
	}
}

func TestStoryboardTimingFallsBackToContainerDuration(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","duration":"N/A"}]}`
	timing := storyboardTiming(&raw, 90)
	if timing.Start != 0 || timing.Duration != 90 {
		t.Fatalf("timing = %#v", timing)
	}
}

func TestSalvageStoryboardSheetsRepeatsLastDecodableSheet(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "asset.tmp-000.webp")
	if err := os.WriteFile(first, []byte("frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := storyboardSheetCount(directory, "asset", 3); got != 1 {
		t.Fatalf("sheet count = %d", got)
	}
	ok, reused := salvageStoryboardSheets(directory, "asset", 3)
	if !ok || reused != 2 {
		t.Fatalf("salvage = %v, reused = %d", ok, reused)
	}
	for index := 0; index < 3; index++ {
		path := filepath.Join(directory, "asset.tmp-00"+strconv.Itoa(index)+".webp")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "frame" {
			t.Fatalf("sheet %d = %q, %v", index, data, err)
		}
	}
}

func TestVideoPosterSeekStaysBeforeShortVideoEnd(t *testing.T) {
	short := 1.0
	veryShort := 0.2
	long := 30.0
	tests := []struct {
		name     string
		duration *float64
		want     float64
	}{
		{name: "unknown", duration: nil, want: 1},
		{name: "one second", duration: &short, want: 0.5},
		{name: "very short", duration: &veryShort, want: 0.1},
		{name: "long", duration: &long, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := videoPosterSeekSeconds(test.duration); math.Abs(got-test.want) > 0.0001 {
				t.Fatalf("seek = %f, want %f", got, test.want)
			}
		})
	}
}

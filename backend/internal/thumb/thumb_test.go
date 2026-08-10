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

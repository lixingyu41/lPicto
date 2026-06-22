package media

import (
	"testing"
	"time"
)

func TestDetectByPath(t *testing.T) {
	image := DetectByPath("IMG.JPG")
	if !image.OK || image.MediaType != "image" || image.MimeType != "image/jpeg" {
		t.Fatalf("image detection = %+v", image)
	}
	video := DetectByPath("clip.mkv")
	if !video.OK || video.MediaType != "video" {
		t.Fatalf("video detection = %+v", video)
	}
	unsupported := DetectByPath("notes.txt")
	if unsupported.OK {
		t.Fatalf("unsupported detection = %+v", unsupported)
	}
}

func TestTimelineAtFallback(t *testing.T) {
	taken := int64(10)
	video := int64(20)
	if got := TimelineAt(&taken, &video, 30, 40); got != 10 {
		t.Fatalf("taken priority = %d", got)
	}
	if got := TimelineAt(nil, &video, 30, 40); got != 20 {
		t.Fatalf("video priority = %d", got)
	}
	if got := TimelineAt(nil, nil, 30, 40); got != 30 {
		t.Fatalf("mtime priority = %d", got)
	}
	if got := TimelineAt(nil, nil, 0, 40); got != 40 {
		t.Fatalf("imported fallback = %d", got)
	}
}

func TestBrowserVideoPlayable(t *testing.T) {
	if !BrowserVideoPlayable("mp4", "h264", "aac") {
		t.Fatal("expected h264/aac mp4 playable")
	}
	if BrowserVideoPlayable("mov", "prores", "pcm_s16le") {
		t.Fatal("expected mov prores not playable")
	}
}

func TestUnixTimeParsesNoZoneInLocalLocation(t *testing.T) {
	previousLocal := time.Local
	local := time.FixedZone("TEST", 8*60*60)
	time.Local = local
	defer func() {
		time.Local = previousLocal
	}()

	got := unixTime("2024:05:01 10:00:00")
	want := time.Date(2024, 5, 1, 10, 0, 0, 0, local).Unix()
	if got == nil || *got != want {
		t.Fatalf("local timestamp = %v, want %d", got, want)
	}

	got = unixTime("2024-05-01T10:00:00Z")
	want = time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC).Unix()
	if got == nil || *got != want {
		t.Fatalf("zoned timestamp = %v, want %d", got, want)
	}
}

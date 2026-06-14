package media

import "testing"

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

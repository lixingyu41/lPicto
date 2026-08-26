package video

import (
	"reflect"
	"slices"
	"testing"
)

func TestHWAccelArgsDisabled(t *testing.T) {
	got := hwAccelArgs("none", "")
	if len(got) != 0 {
		t.Fatalf("args = %#v, want empty", got)
	}
}

func TestHWAccelArgsWithDevice(t *testing.T) {
	got := hwAccelArgs("vaapi", "/dev/dri/renderD128")
	want := []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestStreamProxyArgsUseFastSeekKeyframes(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 0, 23, "none", "", 0)
	for _, want := range []string{"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)"} {
		if !slices.Contains(got, want) {
			t.Fatalf("proxy args = %#v, missing %q", got, want)
		}
	}
}

func TestStreamProxyArgsUseFragmentedMP4(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 1080, 23, "none", "", 0)
	for _, want := range []string{"-progress", "pipe:2", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1"} {
		if !slices.Contains(got, want) {
			t.Fatalf("stream args = %#v, missing %q", got, want)
		}
	}
	if slices.Contains(got, "-re") {
		t.Fatalf("stream args = %#v, should not include realtime input throttle", got)
	}
}

func TestStreamProxyArgsUseStartOffset(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 1080, 23, "none", "", 12.345)
	for _, want := range []string{"-ss", "12.345", "-i", "in.mkv"} {
		if !slices.Contains(got, want) {
			t.Fatalf("stream args = %#v, missing %q", got, want)
		}
	}
}

func TestStreamProxyArgsUseVAAPIEncoder(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 0, 23, "vaapi", "/dev/dri/renderD128", 0)
	for _, want := range []string{"-vaapi_device", "/dev/dri/renderD128", "-vf", "format=nv12,hwupload,scale_vaapi=w=ceil(iw/2)*2:h=ceil(ih/2)*2:format=nv12", "-c:v", "h264_vaapi", "-qp", "23"} {
		if !slices.Contains(got, want) {
			t.Fatalf("stream args = %#v, missing %q", got, want)
		}
	}
}

func TestStreamSegmentArgsUseBoundedMpegTS(t *testing.T) {
	got := StreamSegmentArgs("in.mkv", 1080, 23, "none", "", 80, 10)
	for _, want := range []string{"-ss", "80.000", "-t", "10.000", "-threads", "4", "-f", "mpegts", "-mpegts_flags", "+resend_headers", "pipe:1"} {
		if !slices.Contains(got, want) {
			t.Fatalf("segment args = %#v, missing %q", got, want)
		}
	}
	if slices.Contains(got, "-movflags") {
		t.Fatalf("segment args = %#v, should not use fragmented mp4 output", got)
	}
}

func TestStreamSegmentIgnoreEditListArgsKeepFastSeek(t *testing.T) {
	got := StreamSegmentIgnoreEditListArgs("in.mp4", 1080, 23, "none", "", 80, 10)
	wantOrder := []string{"-ignore_editlist", "1", "-seek_streams_individually", "0", "-ss", "80.000", "-i", "in.mp4"}
	position := -1
	for _, want := range wantOrder {
		found := slices.Index(got[position+1:], want)
		if found < 0 {
			t.Fatalf("segment args = %#v, missing ordered argument %q", got, want)
		}
		position += found + 1
	}
}

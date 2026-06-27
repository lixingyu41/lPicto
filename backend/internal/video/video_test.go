package video

import (
	"reflect"
	"slices"
	"testing"
)

func TestHWAccelArgsDisabled(t *testing.T) {
	got := (Processor{HWAccel: "none"}).hwAccelArgs()
	if len(got) != 0 {
		t.Fatalf("args = %#v, want empty", got)
	}
}

func TestHWAccelArgsWithDevice(t *testing.T) {
	got := (Processor{HWAccel: "vaapi", HWDevice: "/dev/dri/renderD128"}).hwAccelArgs()
	want := []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestProxyArgsUseFastSeekKeyframes(t *testing.T) {
	got := (Processor{ProxyCRF: 23}).cpuProxyTail("in.mkv", "out.mp4", "scale=-2:1080")
	for _, want := range []string{"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)", "+faststart"} {
		if !slices.Contains(got, want) {
			t.Fatalf("proxy args = %#v, missing %q", got, want)
		}
	}
}

func TestStreamProxyArgsUseFragmentedMP4(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 1080, 23, "none", "", 0)
	for _, want := range []string{"-progress", "pipe:2", "-re", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1"} {
		if !slices.Contains(got, want) {
			t.Fatalf("stream args = %#v, missing %q", got, want)
		}
	}
}

func TestStreamProxyArgsUseStartOffset(t *testing.T) {
	got := StreamProxyArgs("in.mkv", 1080, 23, "none", "", 12.345)
	for _, want := range []string{"-ss", "12.345", "-re", "-i", "in.mkv"} {
		if !slices.Contains(got, want) {
			t.Fatalf("stream args = %#v, missing %q", got, want)
		}
	}
}

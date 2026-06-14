package video

import (
	"reflect"
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

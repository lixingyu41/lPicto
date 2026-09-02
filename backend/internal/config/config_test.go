package config

import "testing"

func TestHWAccelEnv(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATA_ROOT", t.TempDir())
	t.Setenv("FFMPEG_HWACCEL", "CUDA")
	t.Setenv("FFMPEG_HWACCEL_DEVICE", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FFmpegHWAccel != "cuda" {
		t.Fatalf("FFmpegHWAccel = %q, want cuda", cfg.FFmpegHWAccel)
	}
	if cfg.FFmpegHWDevice != "0" {
		t.Fatalf("FFmpegHWDevice = %q, want 0", cfg.FFmpegHWDevice)
	}
}

func TestHWAccelEnvInvalidFallsBack(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATA_ROOT", t.TempDir())
	t.Setenv("FFMPEG_HWACCEL", "bad")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FFmpegHWAccel != "none" {
		t.Fatalf("FFmpegHWAccel = %q, want none", cfg.FFmpegHWAccel)
	}
}

func TestResolveStoryboardWorkers(t *testing.T) {
	if got := ResolveStoryboardWorkers(0, "none"); got != 1 {
		t.Fatalf("CPU workers = %d, want 1", got)
	}
	if got := ResolveStoryboardWorkers(0, "vaapi"); got != 2 {
		t.Fatalf("VAAPI workers = %d, want 2", got)
	}
	if got := ResolveStoryboardWorkers(9, "vaapi"); got != 4 {
		t.Fatalf("bounded override = %d, want 4", got)
	}
}

func TestPhotoRootsEnv(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	t.Setenv("PHOTO_ROOTS", "C666="+first+";D666="+second)
	t.Setenv("DATA_ROOT", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PhotoRoots) != 2 {
		t.Fatalf("len = %d, want 2", len(cfg.PhotoRoots))
	}
	if cfg.PhotoRoots[0].ID != "C666" || cfg.PhotoRoots[0].Path != first {
		t.Fatalf("first root = %#v", cfg.PhotoRoots[0])
	}
	if cfg.PhotoRoots[1].ID != "D666" || cfg.PhotoRoots[1].Path != second {
		t.Fatalf("second root = %#v", cfg.PhotoRoots[1])
	}
}

func TestStringMapEnvNormalizesWatcherRoots(t *testing.T) {
	t.Setenv("NAS_WATCHER_ROOTS", "pic=/nas/PIC/; VID=nas\\VID")
	values := stringMapEnv("NAS_WATCHER_ROOTS", "")
	if values["PIC"] != "nas/PIC" || values["VID"] != "nas/VID" {
		t.Fatalf("values = %#v", values)
	}
}

func TestMediaOriginPortsAreValidatedAndDeduplicated(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("DATA_ROOT", t.TempDir())
	t.Setenv("MEDIA_ORIGIN_PORTS", "18081, 18082;18081,0,70000,bad")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MediaOriginPorts) != 2 || cfg.MediaOriginPorts[0] != 18081 || cfg.MediaOriginPorts[1] != 18082 {
		t.Fatalf("MediaOriginPorts = %#v", cfg.MediaOriginPorts)
	}
}

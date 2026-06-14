package config

import "testing"

func TestHWAccelEnv(t *testing.T) {
	t.Setenv("PHOTO_ROOT", t.TempDir())
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
	t.Setenv("PHOTO_ROOT", t.TempDir())
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

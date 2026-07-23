package video

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/util"
)

const defaultVAAPIDevice = "/dev/dri/renderD128"

func ResolveHWAccel(ctx context.Context, requested string, hwDevice string, logger *slog.Logger) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		requested = "none"
	}
	if requested != "auto" {
		return requested
	}
	if ffmpegOutputContains(ctx, "-hwaccels", "vaapi") && ffmpegOutputContains(ctx, "-encoders", "h264_vaapi") && vaapiDeviceAvailable(hwDevice) {
		if logger != nil {
			logger.Info("ffmpeg hardware acceleration selected", "hwAccel", "vaapi", "encoder", "h264_vaapi")
		}
		return "vaapi"
	}
	if ffmpegOutputContains(ctx, "-hwaccels", "cuda") && ffmpegOutputContains(ctx, "-encoders", "h264_nvenc") && cudaDeviceAvailable(ctx) {
		if logger != nil {
			logger.Info("ffmpeg hardware acceleration selected", "hwAccel", "cuda", "encoder", "h264_nvenc")
		}
		return "cuda"
	}
	if logger != nil {
		logger.Info("ffmpeg hardware acceleration unavailable, using CPU", "requested", requested)
	}
	return "none"
}

func ffmpegOutputContains(ctx context.Context, flag string, needle string) bool {
	output, err := util.RunCommand(ctx, 5*time.Second, "ffmpeg", "-hide_banner", flag)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(output)), strings.ToLower(needle))
}

func vaapiDeviceAvailable(hwDevice string) bool {
	info, err := os.Stat(vaapiDevice(hwDevice))
	return err == nil && !info.IsDir()
}

func cudaDeviceAvailable(ctx context.Context) bool {
	if runtime.GOOS == "linux" {
		for _, path := range []string{"/dev/nvidia0", "/dev/nvidiactl", "/dev/nvidia-uvm"} {
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() {
				return true
			}
		}
		return false
	}
	_, err := util.RunCommand(ctx, 5*time.Second, "nvidia-smi", "-L")
	return err == nil
}

func StreamProxyArgs(source string, maxHeight int, crf int, hwAccel string, hwDevice string, startSeconds float64) []string {
	inputArgs := streamInputArgs(source, startSeconds)
	switch strings.ToLower(strings.TrimSpace(hwAccel)) {
	case "cuda":
		args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2"}
		args = append(args, inputArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "h264_nvenc", "-preset", "p2", "-rc", "vbr", "-cq", strconv.Itoa(crf), "-b:v", "0",
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", cpuProxyFilter(maxHeight), "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	case "vaapi":
		args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2", "-vaapi_device", vaapiDevice(hwDevice)}
		args = append(args, inputArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-vf", vaapiProxyFilter(maxHeight),
			"-c:v", "h264_vaapi", "-qp", strconv.Itoa(crf),
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	default:
		args := append([]string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2"}, hwAccelArgs(hwAccel, hwDevice)...)
		args = append(args, inputArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf),
			"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", cpuProxyFilter(maxHeight), "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	}
}

func StreamSegmentArgs(source string, maxHeight int, crf int, hwAccel string, hwDevice string, startSeconds float64, durationSeconds float64) []string {
	return streamSegmentArgs(source, maxHeight, crf, hwAccel, hwDevice, startSeconds, durationSeconds, false)
}

func StreamSegmentIgnoreEditListArgs(source string, maxHeight int, crf int, hwAccel string, hwDevice string, startSeconds float64, durationSeconds float64) []string {
	return streamSegmentArgs(source, maxHeight, crf, hwAccel, hwDevice, startSeconds, durationSeconds, true)
}

func streamSegmentArgs(source string, maxHeight int, crf int, hwAccel string, hwDevice string, startSeconds float64, durationSeconds float64, ignoreEditList bool) []string {
	inputArgs := streamSegmentInputArgs(source, startSeconds, ignoreEditList)
	durationArgs := streamDurationArgs(durationSeconds)
	switch strings.ToLower(strings.TrimSpace(hwAccel)) {
	case "cuda":
		args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2"}
		args = append(args, inputArgs...)
		args = append(args, durationArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "h264_nvenc", "-preset", "p2", "-rc", "vbr", "-cq", strconv.Itoa(crf), "-b:v", "0",
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", cpuProxyFilter(maxHeight), "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-mpegts_flags", "+resend_headers", "-muxdelay", "0", "-muxpreload", "0",
			"-f", "mpegts", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	case "vaapi":
		args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2", "-vaapi_device", vaapiDevice(hwDevice)}
		args = append(args, inputArgs...)
		args = append(args, durationArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-vf", vaapiProxyFilter(maxHeight),
			"-c:v", "h264_vaapi", "-qp", strconv.Itoa(crf),
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-c:a", "aac", "-mpegts_flags", "+resend_headers", "-muxdelay", "0", "-muxpreload", "0",
			"-f", "mpegts", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	default:
		args := append([]string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2"}, hwAccelArgs(hwAccel, hwDevice)...)
		args = append(args, inputArgs...)
		args = append(args, durationArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf),
			"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", cpuProxyFilter(maxHeight), "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-mpegts_flags", "+resend_headers", "-muxdelay", "0", "-muxpreload", "0",
			"-f", "mpegts", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	}
}

func streamSegmentInputArgs(source string, startSeconds float64, ignoreEditList bool) []string {
	args := make([]string, 0, 8)
	if ignoreEditList {
		args = append(args, "-ignore_editlist", "1", "-seek_streams_individually", "0")
	}
	if startSeconds > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSeconds, 'f', 3, 64))
	}
	return append(args, "-i", source)
}

func streamInputArgs(source string, startSeconds float64) []string {
	args := make([]string, 0, 4)
	if startSeconds > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSeconds, 'f', 3, 64))
	}
	return append(args, "-i", source)
}

func streamDurationArgs(durationSeconds float64) []string {
	if durationSeconds <= 0 {
		return nil
	}
	return []string{"-t", strconv.FormatFloat(durationSeconds, 'f', 3, 64)}
}

func hwAccelArgs(hwAccel string, hwDevice string) []string {
	hwAccel = strings.ToLower(strings.TrimSpace(hwAccel))
	if hwAccel == "" || hwAccel == "none" {
		return nil
	}
	args := []string{"-hwaccel", hwAccel}
	if strings.TrimSpace(hwDevice) != "" {
		args = append(args, "-hwaccel_device", hwDevice)
	}
	return args
}

func cpuProxyFilter(maxHeight int) string {
	if maxHeight > 0 {
		return fmt.Sprintf("scale=-2:min(%d\\,trunc(ih/2)*2)", maxHeight)
	}
	return "pad=ceil(iw/2)*2:ceil(ih/2)*2"
}

func vaapiProxyFilter(maxHeight int) string {
	if maxHeight > 0 {
		return fmt.Sprintf("format=nv12,hwupload,scale_vaapi=w=-2:h=min(%d\\,trunc(ih/2)*2):format=nv12", maxHeight)
	}
	return "format=nv12,hwupload,scale_vaapi=w=ceil(iw/2)*2:h=ceil(ih/2)*2:format=nv12"
}

func vaapiDevice(hwDevice string) string {
	hwDevice = strings.TrimSpace(hwDevice)
	if hwDevice == "" {
		return defaultVAAPIDevice
	}
	return hwDevice
}

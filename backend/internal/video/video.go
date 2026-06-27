package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

func ResolveHWAccel(ctx context.Context, requested string, logger *slog.Logger) string {
	if requested != "auto" {
		return requested
	}
	if ffmpegOutputContains(ctx, "-hwaccels", "cuda") && ffmpegOutputContains(ctx, "-encoders", "h264_nvenc") {
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

type Processor struct {
	DB             *db.DB
	Store          storage.Store
	ProxyEnabled   bool
	ProxyMaxHeight int
	ProxyCRF       int
	HWAccel        string
	HWDevice       string
	HWFallback     bool
	CommandTimeout time.Duration
	ProxyTimeout   time.Duration
	Events         *events.Bus
	Logger         *slog.Logger
}

func (p Processor) Handle(ctx context.Context, task jobs.Task) error {
	switch task.Type {
	case "video_proxy":
		return nil
	default:
		return nil
	}
}

func (p Processor) proxy(ctx context.Context, assetID int64) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.MediaType != model.MediaTypeVideo || !p.ProxyEnabled {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusNotRequired, nil)
	}
	dest, err := p.Store.CachePath("video-proxies", asset.CacheKey, "mp4")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusReady, nil)
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	deleted, err := p.deleteIfSourceMissing(ctx, asset, "video_proxy_source_missing")
	if err != nil || deleted {
		return err
	}
	if err := p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusProcessing, nil); err != nil {
		return err
	}
	tmp := dest + ".tmp.mp4"
	_ = os.Remove(tmp)
	filter := fmt.Sprintf("scale=-2:min(%d\\,trunc(ih/2)*2)", p.ProxyMaxHeight)
	args := p.proxyArgs(source, tmp, filter)
	if err := p.runFFmpeg(ctx, p.proxyTimeout(), args, func() []string {
		return p.cpuProxyArgs(source, tmp, filter)
	}); err != nil {
		deleted, deleteErr := p.deleteIfSourceMissing(ctx, asset, "video_proxy_source_missing")
		if deleteErr != nil {
			return deleteErr
		}
		if deleted {
			_ = os.Remove(tmp)
			return nil
		}
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusError, &message)
		_ = os.Remove(tmp)
		return err
	}
	if _, err := p.DB.GetAsset(ctx, assetID); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusError, &message)
		return err
	}
	return p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusReady, nil)
}

func (p Processor) proxyArgs(source string, tmp string, filter string) []string {
	if p.HWAccel == "cuda" {
		return []string{
			"-y", "-hide_banner", "-loglevel", "error",
			"-i", source,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "h264_nvenc", "-preset", "p2", "-rc", "vbr", "-cq", strconv.Itoa(p.ProxyCRF), "-b:v", "0",
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", filter, "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-movflags", "+faststart", "-max_muxing_queue_size", "1024", tmp,
		}
	}
	args := append([]string{"-y", "-hide_banner", "-loglevel", "error"}, p.hwAccelArgs()...)
	args = append(args, p.cpuProxyTail(source, tmp, filter)...)
	return args
}

func StreamProxyArgs(source string, maxHeight int, crf int, hwAccel string, hwDevice string, startSeconds float64) []string {
	filter := fmt.Sprintf("scale=-2:min(%d\\,trunc(ih/2)*2)", maxHeight)
	inputArgs := streamInputArgs(source, startSeconds)
	if hwAccel == "cuda" {
		args := []string{
			"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2",
		}
		args = append(args, inputArgs...)
		return append(args,
			"-map", "0:v:0", "-map", "0:a?",
			"-c:v", "h264_nvenc", "-preset", "p2", "-rc", "vbr", "-cq", strconv.Itoa(crf), "-b:v", "0",
			"-g", "48", "-force_key_frames", "expr:gte(t,n_forced*2)",
			"-vf", filter, "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "-max_muxing_queue_size", "1024", "pipe:1",
		)
	}
	processor := Processor{HWAccel: hwAccel, HWDevice: hwDevice, ProxyCRF: crf}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:2"}, processor.hwAccelArgs()...)
	args = append(args, inputArgs...)
	args = append(args,
		"-map", "0:v:0", "-map", "0:a?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf),
		"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)",
		"-vf", filter, "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "-max_muxing_queue_size", "1024", "pipe:1",
	)
	return args
}

func streamInputArgs(source string, startSeconds float64) []string {
	args := make([]string, 0, 5)
	if startSeconds > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSeconds, 'f', 3, 64))
	}
	args = append(args, "-re", "-i", source)
	return args
}

func (p Processor) cpuProxyArgs(source string, tmp string, filter string) []string {
	return append([]string{"-y", "-hide_banner", "-loglevel", "error"}, p.cpuProxyTail(source, tmp, filter)...)
}

func (p Processor) cpuProxyTail(source string, tmp string, filter string) []string {
	return []string{
		"-i", source,
		"-map", "0:v:0", "-map", "0:a?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(p.ProxyCRF),
		"-g", "48", "-keyint_min", "24", "-sc_threshold", "0", "-force_key_frames", "expr:gte(t,n_forced*2)",
		"-vf", filter, "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-movflags", "+faststart", "-max_muxing_queue_size", "1024", tmp,
	}
}

func (p Processor) runFFmpeg(ctx context.Context, timeout time.Duration, args []string, cpuArgs func() []string) error {
	_, err := util.RunLowPriorityCommand(ctx, timeout, "ffmpeg", args...)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if !p.hwAccelEnabled() || !p.HWFallback {
		return err
	}
	if p.Logger != nil {
		p.Logger.Warn("ffmpeg hardware acceleration failed, retrying with CPU", "hwAccel", p.HWAccel, "error", err)
	}
	_, retryErr := util.RunLowPriorityCommand(ctx, timeout, "ffmpeg", cpuArgs()...)
	if retryErr != nil {
		return fmt.Errorf("hardware decode failed: %v; CPU fallback failed: %w", err, retryErr)
	}
	return nil
}

func (p Processor) hwAccelEnabled() bool {
	return p.HWAccel != "" && p.HWAccel != "none"
}

func (p Processor) hwAccelArgs() []string {
	if !p.hwAccelEnabled() {
		return nil
	}
	args := []string{"-hwaccel", p.HWAccel}
	if p.HWDevice != "" {
		args = append(args, "-hwaccel_device", p.HWDevice)
	}
	return args
}

func (p Processor) proxyTimeout() time.Duration {
	if p.ProxyTimeout > 0 {
		return p.ProxyTimeout
	}
	return 2 * time.Hour
}

func (p Processor) deleteIfSourceMissing(ctx context.Context, asset model.Asset, reason string) (bool, error) {
	root, _, err := p.Store.RootForRel(asset.RelPath)
	if err != nil {
		return false, err
	}
	if !sourceRootAvailable(root.Path) {
		return false, nil
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return false, err
	}
	missing, err := sourceFileMissing(source)
	if err != nil {
		return false, err
	}
	if !missing {
		return false, nil
	}
	if p.Logger != nil {
		p.Logger.Warn("skip video proxy work because source is unavailable", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason)
	}
	return true, nil
}

func sourceRootAvailable(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && info.IsDir()
}

func sourceFileMissing(path string) (bool, error) {
	info, err := os.Stat(filepath.Clean(path))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && !info.IsDir()
}

func publicError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

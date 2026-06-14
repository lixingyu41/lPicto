package video

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

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
	Logger         *slog.Logger
}

func (p Processor) Handle(ctx context.Context, task jobs.Task) error {
	switch task.Type {
	case "video_poster":
		return p.poster(ctx, task.AssetID)
	case "video_proxy":
		return p.proxy(ctx, task.AssetID)
	default:
		return nil
	}
}

func (p Processor) poster(ctx context.Context, assetID int64) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.MediaType != model.MediaTypeVideo {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusNotRequired, nil)
	}
	dest, err := p.Store.CachePath("video-posters", asset.CacheKey, "jpg")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusReady, nil)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusProcessing, nil); err != nil {
		return err
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	tmp := dest + ".tmp.jpg"
	_ = os.Remove(tmp)
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-ss", "1", "-i", source, "-frames:v", "1", "-q:v", "3", tmp}
	if err := p.runFFmpeg(ctx, p.commandTimeout(), args, func() []string {
		return []string{"-y", "-hide_banner", "-loglevel", "error", "-ss", "1", "-i", source, "-frames:v", "1", "-q:v", "3", tmp}
	}); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusError, &message)
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusError, &message)
		return err
	}
	return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusReady, nil)
}

func (p Processor) proxy(ctx context.Context, assetID int64) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.MediaType != model.MediaTypeVideo || !p.ProxyEnabled || asset.BrowserPlayable {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusNotRequired, nil)
	}
	dest, err := p.Store.CachePath("video-proxies", asset.CacheKey, "mp4")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusReady, nil)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusProcessing, nil); err != nil {
		return err
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	tmp := dest + ".tmp.mp4"
	_ = os.Remove(tmp)
	filter := fmt.Sprintf("scale=-2:min(%d\\,trunc(ih/2)*2)", p.ProxyMaxHeight)
	args := p.proxyArgs(source, tmp, filter)
	if err := p.runFFmpeg(ctx, p.proxyTimeout(), args, func() []string {
		return p.cpuProxyArgs(source, tmp, filter)
	}); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_proxy_status", model.StatusError, &message)
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
			"-vf", filter, "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-movflags", "+faststart", "-max_muxing_queue_size", "1024", tmp,
		}
	}
	args := append([]string{"-y", "-hide_banner", "-loglevel", "error"}, p.hwAccelArgs()...)
	args = append(args, p.cpuProxyTail(source, tmp, filter)...)
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
		"-vf", filter, "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-movflags", "+faststart", "-max_muxing_queue_size", "1024", tmp,
	}
}

func (p Processor) runFFmpeg(ctx context.Context, timeout time.Duration, args []string, cpuArgs func() []string) error {
	_, err := util.RunCommand(ctx, timeout, "ffmpeg", args...)
	if err == nil {
		return nil
	}
	if !p.hwAccelEnabled() || !p.HWFallback {
		return err
	}
	if p.Logger != nil {
		p.Logger.Warn("ffmpeg hardware acceleration failed, retrying with CPU", "hwAccel", p.HWAccel, "error", err)
	}
	_, retryErr := util.RunCommand(ctx, timeout, "ffmpeg", cpuArgs()...)
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

func (p Processor) commandTimeout() time.Duration {
	if p.CommandTimeout > 0 {
		return p.CommandTimeout
	}
	return 2 * time.Minute
}

func (p Processor) proxyTimeout() time.Duration {
	if p.ProxyTimeout > 0 {
		return p.ProxyTimeout
	}
	return 2 * time.Hour
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

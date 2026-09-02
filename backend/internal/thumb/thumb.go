package thumb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/cachepolicy"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
	"lpicto/backend/internal/video"
)

type Processor struct {
	DB               *db.DB
	Store            storage.Store
	ThumbLongEdge    int
	PreviewLongEdge  int
	PreviewQuality   int
	CommandTimeout   time.Duration
	Events           *events.Bus
	Logger           *slog.Logger
	Sources          *storage.SourceHealth
	CachePolicy      *cachepolicy.Manager
	FFmpegHWAccel    string
	FFmpegHWDevice   string
	FFmpegHWFallback bool
}

func (p Processor) Handle(ctx context.Context, task jobs.Task) error {
	batchID, _ := p.DB.BeginSourceIOBatch(context.Background(), task.Type, 80)
	var runErr error
	defer func() {
		state, message := "success", ""
		if errors.Is(runErr, jobs.ErrMediaScanPriority) {
			state, message = "preempted", "媒体扫描已抢占后台处理"
		} else if errors.Is(runErr, jobs.ErrPlaybackPriority) || ctx.Err() != nil {
			state, message = "preempted", "当前媒体播放已抢占 NAS 读取"
		} else if runErr != nil {
			state, message = "failed", runErr.Error()
		}
		_ = p.DB.FinishSourceIOBatch(context.Background(), batchID, state, 1, 0, message)
	}()
	switch task.Type {
	case "thumb":
		runErr = p.process(ctx, task.AssetID, "thumbs", "thumb_status", p.ThumbLongEdge, 76)
	case "video_poster":
		runErr = p.processVideoPoster(ctx, task.AssetID)
	case "preview":
		runErr = p.process(ctx, task.AssetID, "previews", "preview_status", p.PreviewLongEdge, p.PreviewQuality)
	case "storyboard":
		runErr = p.processStoryboard(ctx, task.AssetID)
	}
	if shouldAutomaticallyRetry(runErr) && task.Attempt < jobs.MaxAutomaticRetries {
		p.resetForAutomaticRetry(task)
		runErr = errors.Join(jobs.ErrRetryable, runErr)
	}
	return runErr
}

func shouldAutomaticallyRetry(err error) bool {
	return err != nil &&
		!errors.Is(err, jobs.ErrMediaScanPriority) &&
		!errors.Is(err, jobs.ErrMediaCachePriority) &&
		!errors.Is(err, jobs.ErrPlaybackPriority) &&
		!errors.Is(err, jobs.ErrTaskStopped) &&
		!errors.Is(err, context.Canceled)
}

func (p Processor) resetForAutomaticRetry(task jobs.Task) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch task.Type {
	case "thumb":
		_ = p.DB.SetAssetWorkStatus(ctx, task.AssetID, "thumb_status", model.StatusPending, nil)
	case "preview":
		_ = p.DB.SetAssetWorkStatus(ctx, task.AssetID, "preview_status", model.StatusPending, nil)
	case "video_poster":
		_ = p.DB.SetAssetWorkStatus(ctx, task.AssetID, "thumb_status", model.StatusPending, nil)
		_ = p.DB.SetAssetWorkStatus(ctx, task.AssetID, "video_poster_status", model.StatusPending, nil)
	case "storyboard":
		_ = p.DB.SetStoryboardJobStatus(ctx, task.AssetID, model.StatusPending, nil)
	}
}

func interruptedTaskError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), jobs.ErrMediaScanPriority) {
		return jobs.ErrMediaScanPriority
	}
	if errors.Is(context.Cause(ctx), jobs.ErrTaskStopped) {
		return jobs.ErrTaskStopped
	}
	return jobs.ErrPlaybackPriority
}

const (
	storyboardColumns    = 4
	storyboardRows       = 4
	storyboardCellWidth  = 160
	storyboardCellHeight = 90
	storyboardFrameStep  = 15.0
)

func StoryboardLayout(duration float64) (frameCount int, sheetCount int, interval float64) {
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0, 0, 0
	}
	frameCount = int(math.Ceil(duration / storyboardFrameStep))
	if frameCount < storyboardColumns*storyboardRows {
		frameCount = storyboardColumns * storyboardRows
	}
	sheetCount = int(math.Ceil(float64(frameCount) / float64(storyboardColumns*storyboardRows)))
	interval = duration / float64(frameCount)
	return frameCount, sheetCount, interval
}

func StoryboardSheetKey(cacheKey string, index int) string {
	return fmt.Sprintf("%s-%03d", cacheKey, index)
}

type storyboardVideoTiming struct {
	Start    float64
	Duration float64
}

func storyboardTiming(metadataJSON *string, timelineDuration float64) storyboardVideoTiming {
	fallback := storyboardVideoTiming{Duration: timelineDuration}
	if metadataJSON == nil || strings.TrimSpace(*metadataJSON) == "" {
		return fallback
	}
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			StartTime string `json:"start_time"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal([]byte(*metadataJSON), &probe); err != nil {
		return fallback
	}
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" {
			continue
		}
		start, _ := strconv.ParseFloat(strings.TrimSpace(stream.StartTime), 64)
		duration, _ := strconv.ParseFloat(strings.TrimSpace(stream.Duration), 64)
		if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
			start = 0
		}
		if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
			return fallback
		}
		if start > timelineDuration {
			start = timelineDuration
		}
		if duration > timelineDuration-start {
			duration = timelineDuration - start
		}
		if duration <= 0 {
			return fallback
		}
		return storyboardVideoTiming{Start: start, Duration: duration}
	}
	return fallback
}

func storyboardTimelineFilters(asset model.Asset, timelineDuration float64) []string {
	timing := storyboardTiming(asset.MetadataJSON, timelineDuration)
	trailing := math.Max(0, timelineDuration-timing.Start-timing.Duration)
	filters := []string{"setpts=PTS-STARTPTS"}
	if timing.Start > 0.001 || trailing > 0.001 {
		filters = append(filters, fmt.Sprintf(
			"tpad=start_mode=clone:start_duration=%.6f:stop_mode=clone:stop_duration=%.6f",
			timing.Start, trailing,
		))
	}
	return filters
}

func storyboardFilter(asset model.Asset, timelineDuration, interval float64) string {
	filters := storyboardTimelineFilters(asset, timelineDuration)
	filters = append(filters,
		fmt.Sprintf("fps=1/%.6f", interval),
		fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", storyboardCellWidth, storyboardCellHeight),
		fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black", storyboardCellWidth, storyboardCellHeight),
		fmt.Sprintf("tile=%dx%d:nb_frames=%d", storyboardColumns, storyboardRows, storyboardColumns*storyboardRows),
	)
	return strings.Join(filters, ",")
}

func storyboardVAAPIFilter(asset model.Asset, timelineDuration, interval float64) string {
	filters := []string{
		fmt.Sprintf("scale_vaapi=w=%d:h=%d:force_original_aspect_ratio=decrease", storyboardCellWidth, storyboardCellHeight),
		"hwdownload",
		"format=nv12",
	}
	filters = append(filters, storyboardTimelineFilters(asset, timelineDuration)...)
	filters = append(filters,
		fmt.Sprintf("fps=1/%.6f", interval),
		fmt.Sprintf("pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black", storyboardCellWidth, storyboardCellHeight),
		fmt.Sprintf("tile=%dx%d:nb_frames=%d", storyboardColumns, storyboardRows, storyboardColumns*storyboardRows),
	)
	return strings.Join(filters, ",")
}

func copyStoryboardSheet(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

func salvageStoryboardSheets(directory, cacheKey string, sheetCount int) (bool, int) {
	lastReady := ""
	reused := 0
	for index := 0; index < sheetCount; index++ {
		path := fmt.Sprintf(filepath.Join(directory, cacheKey+".tmp-%03d.webp"), index)
		if fileExists(path) {
			lastReady = path
			continue
		}
		if lastReady == "" || copyStoryboardSheet(lastReady, path) != nil {
			return false, reused
		}
		reused++
	}
	return true, reused
}

func storyboardSheetCount(directory, cacheKey string, sheetCount int) int {
	ready := 0
	for index := 0; index < sheetCount; index++ {
		path := fmt.Sprintf(filepath.Join(directory, cacheKey+".tmp-%03d.webp"), index)
		if !fileExists(path) {
			break
		}
		ready++
	}
	return ready
}

func (p Processor) processStoryboard(ctx context.Context, assetID int64) (runErr error) {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.MediaType != model.MediaTypeVideo {
		return p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusNotRequired, nil)
	}
	duration := 0.0
	if asset.Duration != nil {
		duration = *asset.Duration
	}
	_, sheetCount, interval := StoryboardLayout(duration)
	if sheetCount == 0 {
		message := "视频时长不可用，请先执行媒体信息提取"
		_ = p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusError, &message)
		return errors.New(message)
	}
	allReady := true
	for index := 0; index < sheetCount; index++ {
		path, pathErr := p.Store.CacheFilePath("storyboards", StoryboardSheetKey(asset.CacheKey, index), "webp")
		if pathErr != nil || !fileExists(path) {
			allReady = false
			break
		}
	}
	if allReady {
		return p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusReady, nil)
	}
	if p.Sources != nil {
		if available, _ := p.Sources.AvailableForRel(asset.RelPath); !available {
			return nil
		}
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	deleted, err := p.deleteIfSourceMissing(ctx, asset, "storyboard_source_missing")
	if err != nil || deleted {
		return err
	}
	if err := p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusProcessing, nil); err != nil {
		return err
	}
	first, err := p.Store.CachePath("storyboards", StoryboardSheetKey(asset.CacheKey, 0), "webp")
	if err != nil {
		return err
	}
	directory := filepath.Dir(first)
	releaseStoryboardCache := func() {}
	if p.CachePolicy != nil {
		releaseStoryboardCache = p.CachePolicy.Pin(directory)
	}
	defer releaseStoryboardCache()
	tmpPattern := filepath.Join(directory, asset.CacheKey+".tmp-%03d.webp")
	defer func() {
		matches, _ := filepath.Glob(filepath.Join(directory, asset.CacheKey+".tmp-*.webp"))
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}()
	commandTimeout := p.timeout()
	if commandTimeout < 15*time.Minute {
		commandTimeout = 15 * time.Minute
	}
	fastKeyframes := duration >= 60
	runStoryboardOnce := func(keyframesOnly bool, hardware bool) error {
		args := []string{
			"-y", "-hide_banner", "-loglevel", "error",
			"-fflags", "+discardcorrupt+genpts", "-err_detect", "ignore_err",
			"-filter_threads", "2",
		}
		filter := storyboardFilter(asset, duration, interval)
		if hardware {
			args = append(args, video.DecodeInputArgs(p.FFmpegHWAccel, p.FFmpegHWDevice, true)...)
			filter = storyboardVAAPIFilter(asset, duration, interval)
		} else {
			args = append(args, "-threads", "3")
		}
		if keyframesOnly {
			args = append(args, "-skip_frame", "nokey")
		}
		args = append(args, "-i", source,
			"-map", "0:v:0", "-an", "-sn", "-dn",
			"-vf", filter, "-frames:v", fmt.Sprintf("%d", sheetCount), "-start_number", "0", "-c:v", "libwebp", "-threads", "2", "-quality", "42", "-compression_level", "6", tmpPattern)
		_, commandErr := util.RunLowPriorityCommand(ctx, commandTimeout, "ffmpeg", args...)
		return commandErr
	}
	removeTemporarySheets := func() {
		matches, _ := filepath.Glob(filepath.Join(directory, asset.CacheKey+".tmp-*.webp"))
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
	useVAAPI := strings.EqualFold(strings.TrimSpace(p.FFmpegHWAccel), "vaapi")
	runStoryboard := func(keyframesOnly bool) error {
		if useVAAPI {
			hardwareErr := runStoryboardOnce(keyframesOnly, true)
			if hardwareErr == nil || ctx.Err() != nil || !p.FFmpegHWFallback {
				return hardwareErr
			}
			removeTemporarySheets()
			if p.Logger != nil {
				p.Logger.Warn("storyboard hardware decode failed; retrying on CPU", "assetID", assetID, "error", hardwareErr)
			}
		}
		return runStoryboardOnce(keyframesOnly, false)
	}
	err = runStoryboard(fastKeyframes)
	complete := func() bool {
		for index := 0; index < sheetCount; index++ {
			if !fileExists(fmt.Sprintf(filepath.Join(directory, asset.CacheKey+".tmp-%03d.webp"), index)) {
				return false
			}
		}
		return true
	}
	// A damaged or unusual video can expose too few keyframes for the final
	// sheet. Keep every usable sheet and let the salvage pass repeat the last
	// decodable image. A full sequential decode is reserved for inputs where
	// the keyframe pass produced no image at all.
	if fastKeyframes && (err != nil || !complete()) && storyboardSheetCount(directory, asset.CacheKey, sheetCount) == 0 {
		removeTemporarySheets()
		err = runStoryboard(false)
	}
	if ctx.Err() != nil {
		_ = p.DB.SetStoryboardJobStatus(context.Background(), assetID, model.StatusPending, nil)
		return interruptedTaskError(ctx)
	}
	if !complete() {
		salvaged, reused := salvageStoryboardSheets(directory, asset.CacheKey, sheetCount)
		if !salvaged {
			message := "进度预览图生成不完整"
			if err != nil {
				message = publicError(err)
			}
			_ = p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusError, &message)
			return errors.New(message)
		}
		if reused > 0 && p.Logger != nil {
			p.Logger.Warn("storyboard reused nearest decodable sheet", "assetID", assetID, "reused", reused)
		}
	}
	for index := 0; index < sheetCount; index++ {
		tmp := fmt.Sprintf(filepath.Join(directory, asset.CacheKey+".tmp-%03d.webp"), index)
		if !fileExists(tmp) {
			message := "进度预览图生成不完整"
			_ = p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusError, &message)
			return errors.New(message)
		}
		dest, pathErr := p.Store.CachePath("storyboards", StoryboardSheetKey(asset.CacheKey, index), "webp")
		if pathErr != nil {
			return pathErr
		}
		if err := os.Rename(tmp, dest); err != nil {
			message := publicError(err)
			_ = p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusError, &message)
			return err
		}
		if p.CachePolicy != nil {
			id := asset.ID
			p.CachePolicy.Register(context.Background(), "storyboards", asset.CacheKey, dest, &id, 0)
		}
	}
	return p.DB.SetStoryboardJobStatus(ctx, assetID, model.StatusReady, nil)
}

func (p Processor) processVideoPoster(ctx context.Context, assetID int64) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.MediaType != model.MediaTypeVideo {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusNotRequired, nil)
	}
	if err := p.process(ctx, assetID, "thumbs", "thumb_status", p.ThumbLongEdge, 76); err != nil {
		if errors.Is(err, jobs.ErrPlaybackPriority) || errors.Is(err, jobs.ErrTaskStopped) || errors.Is(err, context.Canceled) {
			_ = p.DB.SetAssetWorkStatus(context.Background(), assetID, "video_poster_status", model.StatusPending, nil)
			return interruptedTaskError(ctx)
		}
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusError, &message)
		return err
	}
	refreshed, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if refreshed.MediaType != model.MediaTypeVideo {
		return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusNotRequired, nil)
	}
	return p.DB.SetAssetWorkStatus(ctx, assetID, "video_poster_status", model.StatusReady, nil)
}

func (p Processor) process(ctx context.Context, assetID int64, kind string, statusField string, longEdge int, quality int) error {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if statusField == "preview_status" && asset.MediaType != model.MediaTypeImage {
		return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusNotRequired, nil)
	}
	if statusField == "preview_status" && asset.BrowserPlayable {
		return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusNotRequired, nil)
	}
	if p.Sources != nil {
		if available, _ := p.Sources.AvailableForRel(asset.RelPath); !available {
			if p.Logger != nil {
				p.Logger.Info("skip thumbnail work because storage is unavailable", "assetID", asset.ID, "relPath", asset.RelPath)
			}
			return nil
		}
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	deleted, err := p.deleteIfSourceMissing(ctx, asset, "thumb_source_missing")
	if err != nil || deleted {
		return err
	}
	if statusField == "thumb_status" && asset.MediaType == model.MediaTypeVideo {
		return p.processVideoThumb(ctx, asset, source)
	}
	if asset.MediaType != model.MediaTypeImage {
		return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusNotRequired, nil)
	}
	return p.processAsset(ctx, asset, kind, statusField, longEdge, quality, source)
}

func (p Processor) processVideoThumb(ctx context.Context, asset model.Asset, source string) error {
	if hasVideo, hasAudio, audioCodec := media.StreamKindsFromMetadataJSON(valueOrEmpty(asset.MetadataJSON)); !hasVideo && hasAudio {
		if p.Logger != nil {
			p.Logger.Info("reclassifying audio-only container", "assetID", asset.ID, "relPath", asset.RelPath, "ext", asset.Ext)
		}
		return p.DB.ReclassifyAssetAsAudio(ctx, asset.ID, media.AudioMimeType(asset.Ext), media.BrowserAudioPlayable(asset.Ext, audioCodec))
	}
	dest, err := p.Store.CachePath("thumbs", asset.CacheKey, "webp")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		if err := p.setReady(ctx, asset.ID, "thumb_status"); err != nil {
			return err
		}
		return p.DB.SetAssetWorkStatus(ctx, asset.ID, "video_poster_status", model.StatusReady, nil)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusProcessing, nil); err != nil {
		return err
	}
	tmpFrame := dest + ".tmp.jpg"
	tmpThumb := dest + ".tmp.webp"
	_ = os.Remove(tmpFrame)
	_ = os.Remove(tmpThumb)
	if err := p.extractVideoPosterFrame(ctx, asset, source, tmpFrame); err != nil {
		if ctx.Err() != nil {
			_ = p.DB.SetAssetWorkStatus(context.Background(), asset.ID, "thumb_status", model.StatusPending, nil)
			_ = p.DB.SetAssetWorkStatus(context.Background(), asset.ID, "video_poster_status", model.StatusPending, nil)
			_ = os.Remove(tmpFrame)
			_ = os.Remove(tmpThumb)
			return interruptedTaskError(ctx)
		}
		deleted, deleteErr := p.deleteIfSourceMissing(ctx, asset, "video_thumb_source_missing")
		if deleteErr != nil {
			return deleteErr
		}
		if deleted {
			_ = os.Remove(tmpFrame)
			_ = os.Remove(tmpThumb)
			return nil
		}
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusError, &message)
		_ = os.Remove(tmpFrame)
		_ = os.Remove(tmpThumb)
		return err
	}
	args := []string{tmpFrame, "-s", fmt.Sprintf("%dx%d", p.ThumbLongEdge, p.ThumbLongEdge), "-o", fmt.Sprintf("%s[Q=%d]", tmpThumb, 76)}
	if _, err := util.RunLowPriorityCommand(ctx, p.timeout(), "vipsthumbnail", args...); err != nil {
		if ctx.Err() != nil {
			_ = p.DB.SetAssetWorkStatus(context.Background(), asset.ID, "thumb_status", model.StatusPending, nil)
			_ = os.Remove(tmpFrame)
			_ = os.Remove(tmpThumb)
			return interruptedTaskError(ctx)
		}
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusError, &message)
		_ = os.Remove(tmpFrame)
		_ = os.Remove(tmpThumb)
		return err
	}
	_ = os.Remove(tmpFrame)
	if _, err := p.DB.GetAsset(ctx, asset.ID); err != nil {
		_ = os.Remove(tmpThumb)
		return err
	}
	if err := os.Rename(tmpThumb, dest); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusError, &message)
		return err
	}
	if err := p.setReady(ctx, asset.ID, "thumb_status"); err != nil {
		return err
	}
	if p.CachePolicy != nil {
		assetID := asset.ID
		p.CachePolicy.Register(context.Background(), "thumbs", asset.CacheKey, dest, &assetID, 0)
	}
	return p.DB.SetAssetWorkStatus(ctx, asset.ID, "video_poster_status", model.StatusReady, nil)
}

// CaptureVideoPosterAt replaces a video's cached thumbnail with a frame from
// the requested playback position. The existing thumbnail remains untouched
// until both extraction and compression have completed successfully.
func (p Processor) CaptureVideoPosterAt(ctx context.Context, assetID int64, seekSeconds float64) (model.Asset, error) {
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return model.Asset{}, err
	}
	if asset.MediaType != model.MediaTypeVideo {
		return model.Asset{}, errors.New("资源不是视频")
	}
	if math.IsNaN(seekSeconds) || math.IsInf(seekSeconds, 0) || seekSeconds < 0 {
		return model.Asset{}, errors.New("截图时间无效")
	}
	if asset.Duration != nil && *asset.Duration > 0 && seekSeconds >= *asset.Duration {
		seekSeconds = math.Max(0, *asset.Duration-0.001)
	}
	if p.Sources != nil {
		if available, _ := p.Sources.AvailableForRel(asset.RelPath); !available {
			return model.Asset{}, errors.New("存储不可访问")
		}
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return model.Asset{}, err
	}
	dest, err := p.Store.CachePath("thumbs", asset.CacheKey, "webp")
	if err != nil {
		return model.Asset{}, err
	}
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	tmpFrame := dest + ".manual-" + nonce + ".jpg"
	tmpThumb := dest + ".manual-" + nonce + ".webp"
	defer os.Remove(tmpFrame)
	defer os.Remove(tmpThumb)

	if err := p.extractVideoFrameAt(ctx, source, tmpFrame, seekSeconds, false); err != nil {
		return model.Asset{}, err
	}
	args := []string{tmpFrame, "-s", fmt.Sprintf("%dx%d", p.ThumbLongEdge, p.ThumbLongEdge), "-o", fmt.Sprintf("%s[Q=%d]", tmpThumb, 76)}
	if _, err := util.RunCommand(ctx, p.timeout(), "vipsthumbnail", args...); err != nil {
		return model.Asset{}, err
	}
	if !fileExists(tmpThumb) {
		return model.Asset{}, errors.New("未生成视频封面")
	}
	if err := os.Rename(tmpThumb, dest); err != nil {
		return model.Asset{}, err
	}
	if p.CachePolicy != nil {
		id := asset.ID
		p.CachePolicy.Register(context.Background(), "thumbs", asset.CacheKey, dest, &id, 0)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusReady, nil); err != nil {
		return model.Asset{}, err
	}
	if err := p.DB.SetAssetWorkStatus(ctx, asset.ID, "video_poster_status", model.StatusReady, nil); err != nil {
		return model.Asset{}, err
	}
	refreshed, err := p.DB.GetAsset(ctx, asset.ID)
	if err != nil {
		return model.Asset{}, err
	}
	if p.Events != nil {
		p.Events.Publish(events.Event{Type: "asset_ready", Payload: refreshed})
	}
	return refreshed, nil
}

func videoPosterSeekSeconds(duration *float64) float64 {
	if duration == nil || *duration <= 0 || math.IsNaN(*duration) || math.IsInf(*duration, 0) {
		return 1
	}
	return math.Min(1, *duration/2)
}

func (p Processor) extractVideoPosterFrame(ctx context.Context, asset model.Asset, source, destination string) error {
	seek := videoPosterSeekSeconds(asset.Duration)
	err := p.extractVideoFrameAt(ctx, source, destination, seek, true)
	if err == nil || ctx.Err() != nil || seek == 0 {
		return err
	}
	// Some very short or timestamp-irregular videos have no decodable frame at
	// the preferred seek point. The first frame is a valid poster fallback.
	return p.extractVideoFrameAt(ctx, source, destination, 0, true)
}

func (p Processor) extractVideoFrameAt(ctx context.Context, source, destination string, seek float64, lowPriority bool) error {
	run := func(hardware bool) error {
		_ = os.Remove(destination)
		args := []string{"-y", "-hide_banner", "-loglevel", "error"}
		if hardware {
			args = append(args, video.DecodeInputArgs(p.FFmpegHWAccel, p.FFmpegHWDevice, false)...)
		}
		args = append(args,
			"-ss", strconv.FormatFloat(seek, 'f', 6, 64),
			"-i", source,
			"-frames:v", "1", "-q:v", "3",
			destination,
		)
		var err error
		if lowPriority {
			_, err = util.RunLowPriorityCommand(ctx, p.timeout(), "ffmpeg", args...)
		} else {
			_, err = util.RunCommand(ctx, p.timeout(), "ffmpeg", args...)
		}
		if err != nil {
			return err
		}
		if fileExists(destination) {
			return nil
		}
		return errors.New("FFmpeg 未生成视频封面帧")
	}
	hwAccel := strings.ToLower(strings.TrimSpace(p.FFmpegHWAccel))
	useHardware := hwAccel != "" && hwAccel != "none"
	err := run(useHardware)
	if err == nil || !useHardware || !p.FFmpegHWFallback || ctx.Err() != nil {
		return err
	}
	if p.Logger != nil {
		p.Logger.Warn("video poster hardware decode failed; retrying on CPU", "source", source, "error", err)
	}
	return run(false)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (p Processor) processAsset(ctx context.Context, asset model.Asset, kind string, statusField string, longEdge int, quality int, source string) error {
	dest, err := p.Store.CachePath(kind, asset.CacheKey, "webp")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.setReady(ctx, asset.ID, statusField)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, asset.ID, statusField, model.StatusProcessing, nil); err != nil {
		return err
	}
	tmp := dest + ".tmp.webp"
	_ = os.Remove(tmp)
	args := []string{source, "-s", fmt.Sprintf("%dx%d", longEdge, longEdge), "-o", fmt.Sprintf("%s[Q=%d]", tmp, quality)}
	if _, err := util.RunLowPriorityCommand(ctx, p.timeout(), "vipsthumbnail", args...); err != nil {
		if ctx.Err() != nil {
			_ = p.DB.SetAssetWorkStatus(context.Background(), asset.ID, statusField, model.StatusPending, nil)
			_ = os.Remove(tmp)
			return interruptedTaskError(ctx)
		}
		deleted, deleteErr := p.deleteIfSourceMissing(ctx, asset, "image_thumb_source_missing")
		if deleteErr != nil {
			return deleteErr
		}
		if deleted {
			_ = os.Remove(tmp)
			return nil
		}
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, statusField, model.StatusError, &message)
		_ = os.Remove(tmp)
		return err
	}
	if _, err := p.DB.GetAsset(ctx, asset.ID); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, statusField, model.StatusError, &message)
		return err
	}
	if p.CachePolicy != nil {
		assetID := asset.ID
		p.CachePolicy.Register(context.Background(), kind, asset.CacheKey, dest, &assetID, 0)
	}
	return p.setReady(ctx, asset.ID, statusField)
}

func (p Processor) setReady(ctx context.Context, assetID int64, statusField string) error {
	if err := p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusReady, nil); err != nil {
		return err
	}
	if statusField != "thumb_status" || p.Events == nil {
		return nil
	}
	asset, err := p.DB.GetAsset(ctx, assetID)
	if err != nil {
		return nil
	}
	p.Events.Publish(events.Event{Type: "asset_ready", Payload: asset})
	return nil
}

func (p Processor) timeout() time.Duration {
	if p.CommandTimeout > 0 {
		return p.CommandTimeout
	}
	return 90 * time.Second
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
		p.Sources.RecordSourceError(asset.RelPath, err)
		if storage.IsSourceUnavailable(err) {
			if p.Logger != nil {
				p.Logger.Warn("skip thumbnail work because storage became unavailable", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason)
			}
			return true, nil
		}
		return false, err
	}
	if !missing {
		return false, nil
	}
	if p.Logger != nil {
		p.Logger.Warn("skip thumbnail work because source is unavailable", "assetID", asset.ID, "relPath", asset.RelPath, "reason", reason)
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

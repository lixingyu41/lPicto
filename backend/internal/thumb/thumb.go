package thumb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/events"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type Processor struct {
	DB              *db.DB
	Store           storage.Store
	ThumbLongEdge   int
	PreviewLongEdge int
	PreviewQuality  int
	CommandTimeout  time.Duration
	Events          *events.Bus
	Logger          *slog.Logger
}

func (p Processor) Handle(ctx context.Context, task jobs.Task) error {
	switch task.Type {
	case "thumb":
		return p.process(ctx, task.AssetID, "thumbs", "thumb_status", p.ThumbLongEdge, 76)
	case "preview":
		return p.process(ctx, task.AssetID, "previews", "preview_status", p.PreviewLongEdge, p.PreviewQuality)
	default:
		return nil
	}
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
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
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
	dest, err := p.Store.CachePath("thumbs", asset.CacheKey, "webp")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.setReady(ctx, asset.ID, "thumb_status")
	}
	if err := p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusProcessing, nil); err != nil {
		return err
	}
	tmpFrame := dest + ".tmp.jpg"
	tmpThumb := dest + ".tmp.webp"
	_ = os.Remove(tmpFrame)
	_ = os.Remove(tmpThumb)
	if _, err := util.RunLowPriorityCommand(ctx, p.timeout(), "ffmpeg", "-y", "-hide_banner", "-loglevel", "error", "-ss", "1", "-i", source, "-frames:v", "1", "-q:v", "3", tmpFrame); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, asset.ID, "thumb_status", model.StatusError, &message)
		_ = os.Remove(tmpFrame)
		_ = os.Remove(tmpThumb)
		return err
	}
	args := []string{tmpFrame, "-s", fmt.Sprintf("%dx%d", p.ThumbLongEdge, p.ThumbLongEdge), "-o", fmt.Sprintf("%s[Q=%d]", tmpThumb, 76)}
	if _, err := util.RunLowPriorityCommand(ctx, p.timeout(), "vipsthumbnail", args...); err != nil {
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
	return p.setReady(ctx, asset.ID, "thumb_status")
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

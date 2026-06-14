package thumb

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lpicto/backend/internal/db"
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
	if asset.MediaType != model.MediaTypeImage {
		return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusNotRequired, nil)
	}
	dest, err := p.Store.CachePath(kind, asset.CacheKey, "webp")
	if err != nil {
		return err
	}
	if fileExists(dest) {
		return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusReady, nil)
	}
	if err := p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusProcessing, nil); err != nil {
		return err
	}
	source, err := p.Store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	tmp := dest + ".tmp.webp"
	_ = os.Remove(tmp)
	args := []string{source, "-s", fmt.Sprintf("%dx%d", longEdge, longEdge), "-o", fmt.Sprintf("%s[Q=%d]", tmp, quality)}
	if _, err := util.RunCommand(ctx, p.timeout(), "vipsthumbnail", args...); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusError, &message)
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		message := publicError(err)
		_ = p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusError, &message)
		return err
	}
	return p.DB.SetAssetWorkStatus(ctx, assetID, statusField, model.StatusReady, nil)
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

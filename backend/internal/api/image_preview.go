package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lpicto/backend/internal/debugcontrol"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/util"
)

const viewerPreviewReserveBytes int64 = 16 << 20

type viewerPreviewCall struct {
	done chan struct{}
	err  error
}

func (s *Server) ensureViewerPreview(ctx context.Context, asset model.Asset) error {
	if debugcontrol.BackgroundProcessingPaused() {
		return debugcontrol.ErrBackgroundProcessingPaused
	}
	dest, err := s.store.CachePath("previews", asset.CacheKey, "webp")
	if err != nil {
		return err
	}
	if regularFileExists(dest) {
		return nil
	}

	s.previewMu.Lock()
	if call := s.previewCalls[asset.CacheKey]; call != nil {
		s.previewMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
			return call.err
		}
	}
	call := &viewerPreviewCall{done: make(chan struct{})}
	s.previewCalls[asset.CacheKey] = call
	s.previewMu.Unlock()

	call.err = s.generateViewerPreview(ctx, asset, dest)
	close(call.done)
	s.previewMu.Lock()
	delete(s.previewCalls, asset.CacheKey)
	s.previewMu.Unlock()
	return call.err
}

func (s *Server) generateViewerPreview(ctx context.Context, asset model.Asset, dest string) error {
	select {
	case s.previewSlots <- struct{}{}:
		defer func() { <-s.previewSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if regularFileExists(dest) {
		return nil
	}
	if available, _ := s.sourceHealth.AvailableForRel(asset.RelPath); !available {
		return errors.New("source unavailable")
	}
	source, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		return err
	}
	if _, err := s.cachePolicy.EnsureCapacity(ctx, viewerPreviewReserveBytes); err != nil {
		return err
	}

	tmp := dest + ".viewer.tmp.webp"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)
	longEdge := s.cfg.PreviewLongEdge
	if longEdge <= 0 {
		longEdge = 2560
	}
	quality := s.cfg.PreviewQuality
	if quality <= 0 {
		quality = 82
	}
	args := []string{source, "-s", fmt.Sprintf("%dx%d", longEdge, longEdge), "-o", fmt.Sprintf("%s[Q=%d,effort=0]", tmp, quality)}
	if _, err := util.RunCommand(ctx, 90*time.Second, "vipsthumbnail", args...); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	assetID := asset.ID
	s.cachePolicy.Register(context.Background(), "previews", asset.CacheKey, dest, &assetID, 0)
	_ = s.db.SetAssetWorkStatus(context.Background(), asset.ID, "preview_status", model.StatusReady, nil)
	return nil
}

func regularFileExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && info.Mode().IsRegular()
}

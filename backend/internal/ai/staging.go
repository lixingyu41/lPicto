package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lpicto/backend/internal/cachepolicy"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

const (
	StageBatchLimit          = 100
	StageBatchMaxBytes int64 = 3 << 30
)

type Stager struct {
	DB     *db.DB
	Store  storage.Store
	Policy *cachepolicy.Manager
}

func (s Stager) Prepare(ctx context.Context, asset model.Asset) (*db.AIStage, error) {
	if existing, err := s.DB.AIStageForAsset(ctx, asset.ID, asset.CacheKey); err != nil {
		return nil, err
	} else if existing != nil && existing.State == "ready" {
		full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(existing.StagePath))
		if info, statErr := os.Stat(full); statErr == nil && info.IsDir() {
			return existing, nil
		}
	}
	base, err := s.Store.CachePath("ai-staging", asset.CacheKey, "stage")
	if err != nil {
		return nil, err
	}
	dir := base + ".d"
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if _, err := s.Policy.EnsureCapacity(ctx, 64<<20); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	source, err := s.Store.PhotoPath(asset.RelPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ratios := []float64{0}
	if asset.MediaType == model.MediaTypeVideo {
		ratios = videoSampleRatios(asset.Duration)
		for index, ratio := range ratios {
			timestamp := 0.0
			if asset.Duration != nil {
				timestamp = *asset.Duration * ratio
			}
			out := filepath.Join(dir, fmt.Sprintf("%02d.jpg", index))
			if _, err := util.RunLowPriorityCommand(ctx, 2*time.Minute, "ffmpeg",
				"-nostdin", "-hide_banner", "-loglevel", "error", "-ss", fmt.Sprintf("%.3f", timestamp),
				"-i", source, "-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "3", "-y", out); err != nil {
				_ = os.RemoveAll(dir)
				return nil, err
			}
		}
	} else {
		out := filepath.Join(dir, "00.jpg")
		if _, err := util.RunLowPriorityCommand(ctx, 2*time.Minute, "vipsthumbnail", source,
			"-s", "1280x1280", "-o", out+"[Q=88]"); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
	}
	meta, _ := json.Marshal(map[string]any{"ratios": ratios, "mediaType": asset.MediaType})
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	size, err := directorySize(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	rel, _ := filepath.Rel(s.Store.CacheRoot, dir)
	expires := time.Now().Add(24 * time.Hour).Unix()
	stage := db.AIStage{
		AssetID: asset.ID, CacheKey: asset.CacheKey, State: "ready",
		StagePath: filepath.ToSlash(rel), SizeBytes: size, ExpiresAt: &expires,
	}
	if err := s.DB.UpsertAIStage(ctx, stage); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	assetID := asset.ID
	s.Policy.Register(context.Background(), "ai-staging", asset.CacheKey, filepath.Join(dir, "meta.json"), &assetID, 24*time.Hour)
	return &stage, nil
}

func (s Stager) Remove(ctx context.Context, stage *db.AIStage) {
	if stage == nil {
		return
	}
	full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(stage.StagePath))
	_ = os.RemoveAll(full)
	_ = s.DB.DeleteAIStage(ctx, stage.AssetID)
}

func videoSampleRatios(duration *float64) []float64 {
	value := 0.0
	if duration != nil {
		value = *duration
	}
	switch {
	case value < 10:
		return []float64{0.25, 0.5, 0.75}
	case value <= 60:
		return []float64{0.1, 0.5, 0.9}
	case value <= 300:
		return []float64{0.05, 0.2, 0.35, 0.5, 0.65, 0.8, 0.95}
	case value <= 1200:
		return []float64{0.05, 0.1625, 0.275, 0.3875, 0.5, 0.6125, 0.725, 0.8375, 0.95}
	default:
		return []float64{0.05, 0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95}
	}
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

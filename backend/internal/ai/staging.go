package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/cachepolicy"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

const (
	StageLowWater             = 16
	StageBatchLimit           = 32
	StageBatchMaxBytes  int64 = 512 << 20
	StageMaxBundleBytes int64 = 16 << 20
	StageMaxIdle              = 24 * time.Hour
	StageOrphanGrace          = 10 * time.Minute
)

var stagePrepareLocks [64]sync.Mutex

type Stager struct {
	DB     *db.DB
	Store  storage.Store
	Policy *cachepolicy.Manager
}

func (s Stager) Prepare(ctx context.Context, asset model.Asset) (*db.AIStage, error) {
	lock := &stagePrepareLocks[stageLockIndex(asset.CacheKey)]
	lock.Lock()
	defer lock.Unlock()
	if existing, err := s.DB.AIStageForAsset(ctx, asset.ID, asset.CacheKey); err != nil {
		return nil, err
	} else if existing != nil && existing.State == "ready" {
		full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(existing.StagePath))
		if validStageDirectory(full) {
			return existing, nil
		}
		s.Remove(ctx, existing)
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
	if size > StageMaxBundleBytes {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("AI 暂存输入大小 %d 超出 16 MiB 限制", size)
	}
	rel, _ := filepath.Rel(s.Store.CacheRoot, dir)
	expires := time.Now().Add(StageMaxIdle).Unix()
	stage := db.AIStage{
		AssetID: asset.ID, CacheKey: asset.CacheKey, State: "ready",
		StagePath: filepath.ToSlash(rel), SizeBytes: size, ExpiresAt: &expires,
	}
	if err := s.DB.UpsertAIStage(ctx, stage); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	assetID := asset.ID
	s.Policy.Register(context.Background(), "ai-staging", asset.CacheKey, filepath.Join(dir, "meta.json"), &assetID, 10*time.Minute)
	return &stage, nil
}

func stageLockIndex(cacheKey string) uint8 {
	var value uint8
	for index := 0; index < len(cacheKey); index++ {
		value = value*33 + cacheKey[index]
	}
	return value % uint8(len(stagePrepareLocks))
}

func (s Stager) Remove(ctx context.Context, stage *db.AIStage) {
	if stage == nil {
		return
	}
	full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(stage.StagePath))
	if storage.IsWithinRoot(s.Store.CacheRoot, full) {
		_ = os.RemoveAll(full)
	}
	_ = s.DB.DeleteAIStage(ctx, stage.AssetID)
	_ = s.DB.DeleteCacheEntriesByCacheKey(ctx, "ai-staging", stage.CacheKey)
}

func (s Stager) Pin(stage *db.AIStage) func() {
	if stage == nil || s.Policy == nil {
		return func() {}
	}
	full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(stage.StagePath))
	return s.Policy.Pin(full)
}

// CleanupInterrupted is called before workers start. No analysis can still be
// using a stage marked processing, so every such directory is crash residue.
func (s Stager) CleanupInterrupted(ctx context.Context) error {
	items, err := s.DB.AIStages(ctx)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].State == "processing" {
			s.Remove(ctx, &items[index])
		}
	}
	enabled, enabledErr := s.DB.AIExecutionEnabled(ctx)
	if enabledErr == nil && !enabled {
		if err := s.RemoveReady(ctx); err != nil {
			return err
		}
	}
	if err := s.cleanupOrphanDirectories(ctx, 0); err != nil {
		return err
	}
	return s.DB.DeleteOrphanAIStageCacheEntries(ctx)
}

func (s Stager) RemoveReady(ctx context.Context) error {
	items, err := s.DB.AIStages(ctx)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].State == "ready" {
			s.Remove(ctx, &items[index])
		}
	}
	return nil
}

func (s Stager) CleanupAbandoned(ctx context.Context) error {
	items, err := s.DB.AIStages(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	enabled, enabledErr := s.DB.AIExecutionEnabled(ctx)
	if enabledErr != nil {
		return enabledErr
	}
	for index := range items {
		item := &items[index]
		full := filepath.Join(s.Store.CacheRoot, filepath.FromSlash(item.StagePath))
		expired := !enabled && item.ExpiresAt != nil && *item.ExpiresAt <= now.Unix()
		invalid := item.State != "processing" && !validStageDirectory(full)
		if expired || invalid {
			s.Remove(ctx, item)
		}
	}
	if err := s.cleanupOrphanDirectories(ctx, StageOrphanGrace); err != nil {
		return err
	}
	return s.DB.DeleteOrphanAIStageCacheEntries(ctx)
}

func (s Stager) cleanupOrphanDirectories(ctx context.Context, grace time.Duration) error {
	items, err := s.DB.AIStages(ctx)
	if err != nil {
		return err
	}
	referenced := make(map[string]struct{}, len(items))
	for _, item := range items {
		full := filepath.Clean(filepath.Join(s.Store.CacheRoot, filepath.FromSlash(item.StagePath)))
		referenced[full] = struct{}{}
	}
	root := filepath.Join(s.Store.CacheRoot, "ai-staging")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	cutoff := time.Now().Add(-grace)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".stage.d") {
			return nil
		}
		if _, ok := referenced[filepath.Clean(path)]; ok {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().After(cutoff) {
			return filepath.SkipDir
		}
		_ = os.RemoveAll(path)
		return filepath.SkipDir
	})
}

func validStageDirectory(root string) bool {
	metaBytes, err := os.ReadFile(filepath.Join(root, "meta.json"))
	if err != nil {
		return false
	}
	var meta struct {
		Ratios []float64 `json:"ratios"`
	}
	if json.Unmarshal(metaBytes, &meta) != nil || len(meta.Ratios) == 0 || len(meta.Ratios) > 10 {
		return false
	}
	for index := range meta.Ratios {
		info, err := os.Stat(filepath.Join(root, fmt.Sprintf("%02d.jpg", index)))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
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

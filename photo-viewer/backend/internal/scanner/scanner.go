package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/media"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/storage"
	"lpicto/backend/internal/util"
)

type Scanner struct {
	DB                *db.DB
	Store             storage.Store
	Extractor         media.Extractor
	Jobs              *jobs.Manager
	VideoProxyEnabled bool
	Logger            *slog.Logger

	mu        sync.Mutex
	running   bool
	lastStart int64
	pending   *scanRequest
	progress  Progress
}

type Status struct {
	Running   bool           `json:"running"`
	LastStart int64          `json:"lastStart"`
	LastRun   *model.ScanRun `json:"lastRun"`
	Progress  Progress       `json:"progress"`
}

type Progress struct {
	Reason         string `json:"reason"`
	CurrentRoot    string `json:"currentRoot"`
	CurrentRelPath string `json:"currentRelPath"`
	TotalSeen      int    `json:"totalSeen"`
	AssetsAdded    int    `json:"assetsAdded"`
	AssetsUpdated  int    `json:"assetsUpdated"`
	AssetsDeleted  int    `json:"assetsDeleted"`
	Errors         int    `json:"errors"`
}

type counters struct {
	totalSeen     int
	assetsAdded   int
	assetsUpdated int
	assetsDeleted int
	errors        int
	lastError     *string
}

type scanRequest struct {
	reason      string
	roots       []string
	hasOverride bool
}

func (s *Scanner) Trigger(reason string) bool {
	return s.trigger(reason, nil, false)
}

func (s *Scanner) TriggerRoots(reason string, roots []string) bool {
	return s.trigger(reason, roots, true)
}

func (s *Scanner) trigger(reason string, roots []string, hasOverride bool) bool {
	req := scanRequest{reason: reason, roots: append([]string(nil), roots...), hasOverride: hasOverride}
	s.mu.Lock()
	if s.running {
		s.pending = &req
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.lastStart = util.UnixNow()
	s.progress = Progress{Reason: reason}
	s.mu.Unlock()
	go s.run(context.Background(), req)
	return true
}

func (s *Scanner) StartPeriodic(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Trigger("periodic")
			}
		}
	}()
}

func (s *Scanner) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	status := Status{Running: s.running, LastStart: s.lastStart, Progress: s.progress}
	s.mu.Unlock()
	lastRun, err := s.DB.LastScanRun(ctx)
	if err != nil {
		return Status{}, err
	}
	status.LastRun = lastRun
	return status, nil
}

func (s *Scanner) run(ctx context.Context, req scanRequest) {
	defer func() {
		s.mu.Lock()
		next := s.pending
		if next != nil {
			s.pending = nil
			s.lastStart = util.UnixNow()
			s.progress = Progress{Reason: next.reason}
			s.mu.Unlock()
			go s.run(context.Background(), *next)
			return
		}
		s.running = false
		s.mu.Unlock()
	}()
	logger := s.Logger.With("reason", req.reason)
	runID, err := s.DB.StartScanRun(ctx)
	if err != nil {
		logger.Error("start scan run failed", "error", err)
		return
	}
	activeBefore, err := s.DB.ActiveRelPaths(ctx)
	if err != nil {
		logger.Error("load active assets failed", "error", err)
		message := "读取现有资源失败"
		_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
		return
	}
	var scanRoots []string
	var configuredScanFolders bool
	if req.hasOverride {
		scanRoots, err = db.NormalizeScanFolders(req.roots)
		if err != nil {
			logger.Error("normalize scan roots failed", "error", err)
			message := "扫描文件夹无效"
			_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
			return
		}
		configuredScanFolders = true
	} else {
		scanRoots, configuredScanFolders, err = s.DB.GetScanFolders(ctx)
		if err != nil {
			logger.Error("load scan folders failed", "error", err)
			message := "读取扫描文件夹失败"
			_ = s.DB.FinishScanRun(ctx, runID, db.ScanFinish{Status: "error", Errors: 1, LastError: &message})
			return
		}
	}
	seen := make(map[string]struct{}, len(activeBefore))
	counts := counters{}
	failedRoots := map[string]struct{}{}
	for _, root := range scanRoots {
		walkErr := s.walkRoot(ctx, root, seen, &counts)
		if walkErr != nil {
			failedRoots[root] = struct{}{}
			counts.recordError("扫描目录失败", walkErr)
			logger.Warn("walk failed", "root", root, "error", walkErr)
		}
		if root == "" && walkErr != nil {
			s.walkManifestTopLevel(ctx, seen, &counts)
		}
	}
	deletedAt := util.UnixNow()
	for rel := range activeBefore {
		if _, ok := seen[rel]; ok {
			continue
		}
		inScanScope := db.AssetInScanFolders(rel, scanRoots)
		if !inScanScope && !configuredScanFolders {
			continue
		}
		if inScanScope && assetUnderFailedRoot(rel, failedRoots) {
			continue
		}
		if err := s.DB.MarkDeleted(ctx, rel, deletedAt); err != nil {
			counts.recordError("标记删除失败", err)
			logger.Warn("mark deleted failed", "relPath", rel, "error", err)
			continue
		}
		counts.assetsDeleted++
		s.updateProgressCounts(counts, rel)
	}
	if err := s.DB.RefreshFolders(ctx); err != nil {
		counts.recordError("更新文件夹统计失败", err)
		logger.Warn("refresh folders failed", "error", err)
	}
	status := "finished"
	if counts.errors > 0 {
		status = "finished_with_errors"
	}
	if err := s.DB.FinishScanRun(ctx, runID, db.ScanFinish{
		Status:        status,
		TotalSeen:     counts.totalSeen,
		AssetsAdded:   counts.assetsAdded,
		AssetsUpdated: counts.assetsUpdated,
		AssetsDeleted: counts.assetsDeleted,
		Errors:        counts.errors,
		LastError:     counts.lastError,
	}); err != nil {
		logger.Error("finish scan run failed", "error", err)
	}
	logger.Info("scan finished", "seen", counts.totalSeen, "added", counts.assetsAdded, "updated", counts.assetsUpdated, "deleted", counts.assetsDeleted, "errors", counts.errors)
}

func (s *Scanner) walkRoot(ctx context.Context, rootRel string, seen map[string]struct{}, counts *counters) error {
	s.updateProgressRoot(rootRel)
	rootPath, err := s.Store.PhotoPath(rootRel)
	if err != nil {
		return err
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := s.DB.EnsureFolder(ctx, rootRel); err != nil {
		counts.recordError("写入目录失败", err)
	}
	return s.walkDir(ctx, rootPath, seen, counts)
}

func (s *Scanner) walkDir(ctx context.Context, dirPath string, seen map[string]struct{}, counts *counters) error {
	entries, readErr := util.ReadDirPartial(dirPath)
	if readErr != nil {
		counts.recordError("读取目录项失败", readErr)
		s.Logger.Warn("walk entry failed", "path", dirPath, "error", readErr)
	}
	for _, entry := range entries {
		absPath := filepath.Join(dirPath, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			_ = s.handleSymlink(ctx, absPath, entry, seen, counts)
			continue
		}
		if entry.IsDir() {
			if err := s.ensureFolderForPath(ctx, absPath, counts); err != nil {
				continue
			}
			if err := s.walkDir(ctx, absPath, seen, counts); err != nil {
				readErr = err
			}
			continue
		}
		if !media.DetectByPath(entry.Name()).OK {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			counts.recordError("读取文件信息失败", err)
			s.Logger.Warn("file info failed", "path", absPath, "error", err)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		s.processFile(ctx, absPath, info, seen, counts)
	}
	return readErr
}

func (s *Scanner) walkManifestTopLevel(ctx context.Context, seen map[string]struct{}, counts *counters) {
	folders, err := storage.LoadSourceFolderManifest(s.Store.DataRoot)
	if err != nil {
		s.Logger.Warn("load source folder manifest failed", "error", err)
		return
	}
	for _, rel := range storage.ManifestTopLevelFolders(folders) {
		if err := s.walkRoot(ctx, rel, seen, counts); err != nil {
			counts.recordError("扫描目录失败", err)
			s.Logger.Warn("manifest folder walk failed", "root", rel, "error", err)
		}
	}
}

func (s *Scanner) handleSymlink(ctx context.Context, absPath string, entry fs.DirEntry, seen map[string]struct{}, counts *counters) error {
	inside, target, err := storage.SymlinkTargetWithinRoot(s.Store.PhotoRoot, absPath)
	if err != nil {
		counts.recordError("解析符号链接失败", err)
		s.Logger.Warn("symlink eval failed", "path", absPath, "error", err)
		return nil
	}
	if !inside {
		s.Logger.Warn("symlink skipped because it escapes photo root", "path", absPath, "target", target)
		return nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		counts.recordError("读取符号链接目标失败", err)
		return nil
	}
	if info.IsDir() {
		s.Logger.Warn("directory symlink skipped to avoid cycles", "path", absPath)
		return filepath.SkipDir
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	s.processFile(ctx, absPath, info, seen, counts)
	_ = entry
	return nil
}

func assetUnderFailedRoot(rel string, failedRoots map[string]struct{}) bool {
	for root := range failedRoots {
		if root == "" || rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func (s *Scanner) ensureFolderForPath(ctx context.Context, absPath string, counts *counters) error {
	rel, err := s.Store.RelPath(absPath)
	if err != nil {
		counts.recordError("目录路径不安全", err)
		return nil
	}
	if err := s.DB.EnsureFolder(ctx, rel); err != nil {
		counts.recordError("写入目录失败", err)
		return nil
	}
	return nil
}

func (s *Scanner) processFile(ctx context.Context, absPath string, info os.FileInfo, seen map[string]struct{}, counts *counters) {
	rel, err := s.Store.RelPath(absPath)
	if err != nil {
		counts.recordError("文件路径不安全", err)
		return
	}
	detection := media.DetectByPath(info.Name())
	if !detection.OK {
		return
	}
	seen[rel] = struct{}{}
	counts.totalSeen++
	s.updateProgressCounts(*counts, rel)
	if err := s.DB.EnsureAssetFolders(ctx, rel); err != nil {
		counts.recordError("写入文件夹失败", err)
		s.updateProgressCounts(*counts, rel)
		s.Logger.Warn("ensure folders failed", "relPath", rel, "error", err)
		return
	}
	importedAt := util.UnixNow()
	mtime := info.ModTime().Unix()
	meta := s.Extractor.Extract(ctx, absPath, detection, mtime, importedAt)
	mimeType := detection.MimeType
	if meta.MimeType != "" {
		mimeType = meta.MimeType
	}
	browserPlayable := meta.BrowserPlayable
	if detection.MediaType == model.MediaTypeImage {
		browserPlayable = media.BrowserImageDisplayable(mimeType)
	}
	thumbStatus, previewStatus, posterStatus, proxyStatus := db.AssetStatuses(detection.MediaType, browserPlayable, s.VideoProxyEnabled)
	var metadataJSON *string
	if meta.RawJSON != "" {
		metadataJSON = &meta.RawJSON
	}
	var errorText *string
	if meta.Err != nil {
		text := "元数据提取失败"
		errorText = &text
		counts.recordError(text, meta.Err)
		s.updateProgressCounts(*counts, rel)
		s.Logger.Warn("metadata extraction failed", "relPath", rel, "error", meta.Err)
	}
	params := db.AssetUpsert{
		RelPath:           rel,
		ParentRelPath:     storage.ParentRelPath(rel),
		Filename:          filepath.Base(absPath),
		Ext:               detection.Ext,
		MediaType:         detection.MediaType,
		MimeType:          &mimeType,
		Size:              info.Size(),
		Mtime:             mtime,
		Width:             meta.Width,
		Height:            meta.Height,
		Duration:          meta.Duration,
		TakenAt:           meta.TakenAt,
		ImportedAt:        importedAt,
		TimelineAt:        media.TimelineAt(meta.TakenAt, meta.VideoCreatedAt, mtime, importedAt),
		CacheKey:          storage.CacheKey(rel, info.Size(), mtime),
		BrowserPlayable:   browserPlayable,
		ThumbStatus:       thumbStatus,
		PreviewStatus:     previewStatus,
		VideoPosterStatus: posterStatus,
		VideoProxyStatus:  proxyStatus,
		MetadataJSON:      metadataJSON,
		Error:             errorText,
	}
	assetID, added, updated, err := s.DB.UpsertAsset(ctx, params)
	if err != nil {
		counts.recordError("写入资源失败", err)
		s.updateProgressCounts(*counts, rel)
		s.Logger.Warn("upsert asset failed", "relPath", rel, "error", err)
		return
	}
	if added {
		counts.assetsAdded++
	}
	if updated {
		counts.assetsUpdated++
	}
	s.updateProgressCounts(*counts, rel)
	if added || updated {
		s.enqueueWork(assetID, detection.MediaType, proxyStatus)
	}
}

func (s *Scanner) updateProgressRoot(rootRel string) {
	s.mu.Lock()
	s.progress.CurrentRoot = rootRel
	s.mu.Unlock()
}

func (s *Scanner) updateProgressCounts(counts counters, currentRelPath string) {
	s.mu.Lock()
	s.progress.CurrentRelPath = currentRelPath
	s.progress.TotalSeen = counts.totalSeen
	s.progress.AssetsAdded = counts.assetsAdded
	s.progress.AssetsUpdated = counts.assetsUpdated
	s.progress.AssetsDeleted = counts.assetsDeleted
	s.progress.Errors = counts.errors
	s.mu.Unlock()
}

func (s *Scanner) enqueueWork(assetID int64, mediaType string, proxyStatus string) {
	if s.Jobs == nil {
		return
	}
	if mediaType == model.MediaTypeImage {
		s.Jobs.Enqueue(jobs.Task{Type: "thumb", AssetID: assetID})
		s.Jobs.Enqueue(jobs.Task{Type: "preview", AssetID: assetID})
		return
	}
	if mediaType == model.MediaTypeVideo {
		s.Jobs.Enqueue(jobs.Task{Type: "video_poster", AssetID: assetID})
		if proxyStatus == model.StatusPending {
			s.Jobs.Enqueue(jobs.Task{Type: "video_proxy", AssetID: assetID})
		}
	}
}

func (c *counters) recordError(publicMessage string, err error) {
	c.errors++
	if c.lastError == nil {
		if strings.TrimSpace(publicMessage) == "" {
			publicMessage = "扫描失败"
		}
		c.lastError = &publicMessage
	}
	_ = errors.Unwrap(err)
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	aiworker "lpicto/backend/internal/ai"
	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/storage"
)

const (
	taskStorageHealth = "storage_health_check"
	taskCacheCleanup  = "cache_cleanup"
	taskDuplicateScan = "duplicate_scan"
)

type systemTaskRunRequest struct {
	Action    string  `json:"action"`
	LibraryID *string `json:"libraryId"`
}

func (s *Server) runSystemTask(w http.ResponseWriter, r *http.Request) {
	var payload systemTaskRunRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	action := strings.TrimSpace(payload.Action)
	roots, libraryName, ok := s.systemTaskRoots(w, r, payload.LibraryID)
	if !ok {
		return
	}
	reason := "task:" + taskID
	if libraryName != "" {
		reason += ":" + libraryName
	}
	running := s.systemTaskRunning(r.Context(), taskID)
	retryOnly := action == "retry_failed" || action == "reset_failed"
	if systemTaskNeedsStorage(taskID) && !retryOnly {
		for _, root := range roots {
			if status := s.probeConfiguredLibraryRoot(r.Context(), libraryName, root); !status.Available {
				writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "存储不可访问，任务没有启动")
				return
			}
		}
	}
	if action != "stop" && action != "check" && action != "restart" && !retryOnly && running {
		writeJSON(w, http.StatusConflict, map[string]any{"accepted": false, "state": "running", "message": "任务正在运行，请先停止当前任务"})
		return
	}

	switch taskID {
	case "media_scan":
		if action != "continue" && action != "start" && action != "retry_failed" && action != "rebuild" {
			systemTaskActionError(w)
			return
		}
		result := s.scanner.RequestCountScanRoots(reason, roots)
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "library_scan":
		if action != "reconcile" && action != "scan" {
			systemTaskActionError(w)
			return
		}
		result := s.scanner.RequestReconcileScanRoots(reason, roots)
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "metadata_extraction":
		s.runMetadataTask(w, r, action, roots, reason, running)
	case "thumbnail_creation":
		s.runThumbnailTask(w, r, action, roots, reason, running)
	case "preview_creation", "video_poster_creation", "storyboard_creation":
		taskType := "preview"
		if taskID == "video_poster_creation" {
			taskType = "video_poster"
		} else if taskID == "storyboard_creation" {
			taskType = "storyboard"
		}
		s.runQueuedMediaTask(w, r, taskType, action, roots, running)
	case "ai_analysis":
		s.runAIAnalysisTask(w, r, action, running)
	case taskDuplicateScan:
		if action != "scan" && action != "continue" {
			systemTaskActionError(w)
			return
		}
		if !s.startDuplicateScan() {
			writeJSON(w, http.StatusConflict, map[string]any{"accepted": false, "state": "running", "message": "重复文件扫描正在运行"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": "running"})
	case taskStorageHealth:
		if action != "check" {
			systemTaskActionError(w)
			return
		}
		s.runStorageHealthTask(w, r)
	case "ai_health_check":
		s.runAIHealthTask(w, r, action)
	case taskCacheCleanup:
		if action != "cleanup" {
			systemTaskActionError(w)
			return
		}
		if !s.startOrphanCacheCleanup() {
			writeJSON(w, http.StatusConflict, map[string]any{"accepted": false, "state": "running"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": "running"})
	default:
		writeError(w, http.StatusNotFound, "task_not_found", "任务不存在")
	}
}

func (s *Server) systemTaskRunning(ctx context.Context, taskID string) bool {
	if taskID == taskCacheCleanup {
		s.cleanupMu.Lock()
		running := s.cleanupStatus.Running
		s.cleanupMu.Unlock()
		return running
	}
	if taskID == "media_scan" || taskID == "library_scan" || taskID == "metadata_extraction" || taskID == "thumbnail_creation" {
		status, _ := s.scanner.Status(ctx)
		if status.Running && (taskID != "media_scan" || status.Progress.Task == "count") {
			return true
		}
	}
	if taskID == taskDuplicateScan {
		return s.duplicateScanIsRunning()
	}
	if s.jobs == nil {
		return false
	}
	stats := s.jobs.Stats()
	switch taskID {
	case "thumbnail_creation":
		return stats.ThumbQueued+stats.ActiveThumb > 0
	case "preview_creation":
		return stats.PreviewQueued+stats.ActivePreview > 0
	case "video_poster_creation":
		return stats.VideoPosterQueued+stats.ActiveVideoPoster > 0
	case "storyboard_creation":
		return stats.StoryboardQueued+stats.ActiveStoryboard > 0
	case "ai_analysis":
		return stats.AIQueued+stats.ActiveAI > 0
	default:
		return false
	}
}

func systemTaskNeedsStorage(taskID string) bool {
	switch taskID {
	case "media_scan", "library_scan", "metadata_extraction", "thumbnail_creation", "preview_creation", "video_poster_creation", "storyboard_creation", "ai_analysis", taskDuplicateScan:
		return true
	default:
		return false
	}
}

func (s *Server) stopSystemTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	ctx := r.Context()
	switch taskID {
	case "media_scan", "library_scan", "metadata_extraction":
		result := s.scanner.RequestStop()
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "thumbnail_creation":
		_ = s.jobs.ClearQueues(ctx, "thumb", "video_poster")
		s.jobs.CancelActive("thumb", "video_poster")
		_ = s.db.RequeueProcessingWork(ctx, "thumb")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
		result := s.scanner.RequestStop()
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "preview_creation":
		_ = s.jobs.ClearQueues(ctx, "preview")
		s.jobs.CancelActive("preview")
		_ = s.db.RequeueProcessingWork(ctx, "preview")
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case "video_poster_creation":
		_ = s.jobs.ClearQueues(ctx, "video_poster")
		s.jobs.CancelActive("video_poster")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case "storyboard_creation":
		_ = s.jobs.ClearQueues(ctx, "storyboard")
		s.jobs.CancelActive("storyboard")
		_ = s.db.RequeueProcessingWork(ctx, "storyboard")
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case "ai_analysis":
		_, _ = s.db.SetAIAutoAnalyze(ctx, false)
		_, _ = s.db.SetAIManualRun(ctx, false)
		if err := s.jobs.ClearAIQueue(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_stop_failed", "停止 AI 分析失败")
			return
		}
		s.jobs.CancelActive("ai_analyze")
		s.removeQueuedAIStages(ctx)
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "AI 分析已停止；自动分析同时关闭")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case taskDuplicateScan:
		if !s.stopDuplicateScan() {
			writeJSON(w, http.StatusOK, map[string]any{"accepted": false, "state": "idle"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": "stopping"})
	case taskCacheCleanup:
		s.cleanupMu.Lock()
		cancel := s.cleanupCancel
		s.cleanupMu.Unlock()
		if cancel == nil {
			writeJSON(w, http.StatusOK, map[string]any{"accepted": false, "state": "idle"})
			return
		}
		cancel()
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": "stopping"})
	default:
		writeError(w, http.StatusBadRequest, "task_not_stoppable", "这个任务不能停止")
	}
}

func (s *Server) duplicateScanIsRunning() bool {
	s.duplicateScanMu.Lock()
	defer s.duplicateScanMu.Unlock()
	return s.duplicateScanCancel != nil
}

func (s *Server) duplicateScanFailureCount() int {
	s.duplicateScanMu.Lock()
	defer s.duplicateScanMu.Unlock()
	return s.duplicateScanFailed
}

func (s *Server) startDuplicateScan() bool {
	s.duplicateScanMu.Lock()
	if s.duplicateScanCancel != nil {
		s.duplicateScanMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.duplicateScanCancel = cancel
	s.duplicateScanFailed = 0
	s.duplicateScanMu.Unlock()

	_ = s.db.BeginSystemTask(context.Background(), taskDuplicateScan)
	go s.runDuplicateScan(ctx)
	return true
}

func (s *Server) stopDuplicateScan() bool {
	s.duplicateScanMu.Lock()
	cancel := s.duplicateScanCancel
	s.duplicateScanMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (s *Server) runDuplicateScan(ctx context.Context) {
	processed := 0
	failed := 0
	afterID := int64(0)
	defer func() {
		s.duplicateScanMu.Lock()
		s.duplicateScanCancel = nil
		s.duplicateScanFailed = failed
		s.duplicateScanMu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			_ = s.db.FinishSystemTask(context.Background(), taskDuplicateScan, "stopped", fmt.Sprintf("已停止；本次完成 %d 项，%d 项读取失败", processed, failed))
			return
		}
		candidates, err := s.db.DuplicateHashCandidatesAfterID(ctx, afterID, 100)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				_ = s.db.FinishSystemTask(context.Background(), taskDuplicateScan, "stopped", fmt.Sprintf("已停止；本次完成 %d 项，%d 项读取失败", processed, failed))
				return
			}
			_ = s.db.FinishSystemTask(context.Background(), taskDuplicateScan, "failed", "读取重复文件候选失败："+err.Error())
			return
		}
		if len(candidates) == 0 {
			state := "success"
			message := fmt.Sprintf("扫描完成；本次计算 %d 项", processed)
			if failed > 0 {
				state = "warning"
				message = fmt.Sprintf("扫描完成；本次计算 %d 项，%d 项暂时无法读取", processed, failed)
			}
			_ = s.db.FinishSystemTask(context.Background(), taskDuplicateScan, state, message)
			return
		}
		for _, asset := range candidates {
			afterID = asset.ID
			if err := ctx.Err(); err != nil {
				break
			}
			absPath, err := s.store.PhotoPath(asset.RelPath)
			if err == nil {
				var hash string
				hash, err = fileSHA256Hex(absPath)
				if err == nil {
					err = s.db.SetAssetSHA256Hex(ctx, asset.ID, hash)
				}
			}
			if err != nil {
				failed++
				s.duplicateScanMu.Lock()
				s.duplicateScanFailed = failed
				s.duplicateScanMu.Unlock()
				continue
			}
			processed++
		}
	}
}

func (s *Server) runMetadataTask(w http.ResponseWriter, r *http.Request, action string, roots []string, reason string, running bool) {
	switch action {
	case "start":
		result := s.scanner.RequestMetadataScanRoots(reason, roots)
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "continue":
		paths, err := s.db.MetadataWorkPathsForRoots(r.Context(), roots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "metadata_work_failed", "读取待处理媒体失败")
			return
		}
		if len(paths) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "count": 0, "state": "complete"})
			return
		}
		if running {
			result := s.scanner.RequestMetadataScanPaths(reason, roots, paths)
			writeJSON(w, http.StatusAccepted, map[string]any{"accepted": result.Accepted, "count": len(paths), "state": result.State})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(paths), "state": "pending"})
	case "retry_failed":
		paths, err := s.db.RetryFailedMetadataPathsForRoots(r.Context(), roots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "metadata_retry_failed", "重试媒体信息失败任务失败")
			return
		}
		if len(paths) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "count": 0, "state": "complete"})
			return
		}
		result := s.scanner.RequestMetadataScanPaths(reason, roots, paths)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": result.Accepted, "count": len(paths), "state": result.State})
	case "rebuild":
		count, err := s.db.ResetMetadataForRoots(r.Context(), roots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "metadata_reset_failed", "重置媒体信息任务失败")
			return
		}
		result := s.scanner.RequestMetadataScanRoots(reason, roots)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": result.Accepted, "count": count, "state": result.State})
	default:
		systemTaskActionError(w)
	}
}

func (s *Server) runThumbnailTask(w http.ResponseWriter, r *http.Request, action string, roots []string, reason string, running bool) {
	if action != "retry_failed" {
		_ = s.db.BeginSystemTask(context.Background(), "thumbnail_creation")
	}
	var result any
	switch action {
	case "start", "continue":
		result = scanCommandResponse(s.scanner.RequestThumbnailContinueRoots(reason, roots))
	case "retry_failed":
		items, err := s.db.RetryFailedWorkForRoots(r.Context(), "thumb", roots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "thumbnail_retry_failed", "重试缩略图失败任务失败")
			return
		}
		if running {
			for _, item := range items {
				s.jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
			}
		}
		state := "pending"
		if running {
			state = "queued"
		}
		result = map[string]any{"accepted": true, "count": len(items), "state": state}
	case "rebuild":
		result = scanCommandResponse(s.scanner.RequestThumbnailRebuildRoots(reason, roots))
	default:
		systemTaskActionError(w)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) runQueuedMediaTask(w http.ResponseWriter, r *http.Request, taskType string, action string, roots []string, running bool) {
	if action != "start" && action != "continue" && action != "retry_failed" && action != "rebuild" {
		systemTaskActionError(w)
		return
	}
	taskID := "preview_creation"
	if taskType == "video_poster" {
		taskID = "video_poster_creation"
	} else if taskType == "storyboard" {
		taskID = "storyboard_creation"
	}
	if action != "retry_failed" {
		_ = s.db.BeginSystemTask(r.Context(), taskID)
	}
	var items []db.WorkItem
	var err error
	if action == "rebuild" {
		if _, err := s.db.ResetWorkForRoots(r.Context(), taskType, roots); err != nil {
			writeError(w, http.StatusInternalServerError, "task_reset_failed", "重置任务失败")
			return
		}
	} else if action == "retry_failed" {
		items, err = s.db.RetryFailedWorkForRoots(r.Context(), taskType, roots)
	} else {
		items, err = s.db.ContinueWorkForRoots(r.Context(), taskType, roots)
	}
	if items == nil && err == nil {
		items, err = s.db.WorkItemsForRoots(r.Context(), taskType, roots)
	}
	if action == "rebuild" && taskType == "storyboard" {
		for _, item := range items {
			if asset, assetErr := s.db.GetAsset(r.Context(), item.AssetID); assetErr == nil {
				_ = s.store.RemoveCachePrefix(asset.CacheKey, "storyboards", "webp")
			}
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "task_work_failed", "读取待处理媒体失败")
		return
	}
	if action != "retry_failed" || running {
		for _, item := range items {
			s.jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
		}
	}
	state := "pending"
	if action != "retry_failed" || running {
		state = "queued"
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(items), "state": state})
}

func (s *Server) runAIAnalysisTask(w http.ResponseWriter, r *http.Request, action string, running bool) {
	switch action {
	case "continue":
		_ = s.db.BeginSystemTask(r.Context(), "ai_analysis")
		settings, err := s.db.SetAIManualRun(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_manual_run_failed", "启动 AI 分析失败")
			return
		}
		if err := s.db.ResetAIProcessing(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
			return
		}
		failedItems, err := s.db.RetryFailedAI(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
			return
		}
		queued := make(map[int64]struct{}, len(failedItems)+1000)
		for _, item := range failedItems {
			queued[item.AssetID] = struct{}{}
		}
		pendingItems, err := s.db.AIBackfillBatch(r.Context(), 1000)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
			return
		}
		for _, item := range pendingItems {
			if err := s.db.EnsureAIQueued(r.Context(), item.AssetID, item.CacheKey, false); err != nil {
				writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
				return
			}
			if _, exists := queued[item.AssetID]; exists {
				continue
			}
			queued[item.AssetID] = struct{}{}
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(queued), "state": "preparing", "settings": settings})
	case "retry_failed", "reset_failed":
		items, err := s.db.RetryFailedAI(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_retry_failed", "重置 AI 失败任务失败")
			return
		}
		if running && len(items) > 0 {
			_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
		}
		state := "pending"
		if running {
			state = "queued"
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(items), "state": state})
	case "rebuild":
		count, err := s.db.ReindexAI(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_rebuild_failed", "重建 AI 分析任务失败")
			return
		}
		_ = s.db.BeginSystemTask(r.Context(), "ai_analysis")
		_, _ = s.db.SetAIManualRun(r.Context(), true)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": count, "queued": 0, "state": "preparing"})
	default:
		systemTaskActionError(w)
	}
}

func (s *Server) runStorageHealthTask(w http.ResponseWriter, r *http.Request) {
	statuses, failed, message := s.checkStorageHealth(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "failed": failed, "items": statuses, "message": message})
}

func (s *Server) checkStorageHealth(ctx context.Context) ([]storage.SourceHealthStatus, int, string) {
	_ = s.db.BeginSystemTask(ctx, taskStorageHealth)
	libraries, _, _ := s.db.GetScanLibraries(ctx)
	statuses := make([]storage.SourceHealthStatus, 0, len(libraries))
	for _, library := range libraries {
		for index, root := range library.Roots {
			label := library.Name
			if len(library.Roots) > 1 {
				label = fmt.Sprintf("%s · %d", library.Name, index+1)
			}
			statuses = append(statuses, s.probeConfiguredLibraryRoot(ctx, label, root))
		}
	}
	if len(statuses) == 0 {
		statuses = s.sourceHealth.Statuses()
	}
	failed := 0
	for _, status := range statuses {
		if !status.Available {
			failed++
		}
	}
	state, message := "success", fmt.Sprintf("已检查 %d 个存储来源，全部可访问", len(statuses))
	if failed > 0 {
		state, message = "failed", fmt.Sprintf("%d 个存储来源不可访问", failed)
		s.scanner.RequestStop()
		_ = s.jobs.ClearQueues(ctx, "thumb", "preview", "video_poster", "storyboard")
		_ = s.jobs.ClearAIQueue(ctx)
		_ = s.db.RequeueProcessingWork(ctx, "thumb")
		_ = s.db.RequeueProcessingWork(ctx, "preview")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
		_ = s.db.RequeueProcessingWork(ctx, "storyboard")
	}
	_ = s.db.FinishSystemTask(ctx, taskStorageHealth, state, message)
	return statuses, failed, message
}

func (s *Server) probeConfiguredLibraryRoot(ctx context.Context, label string, rel string) storage.SourceHealthStatus {
	status := storage.SourceHealthStatus{RootID: label, Available: true, CheckedAt: time.Now().Unix()}
	path, err := s.store.PhotoPath(rel)
	if err == nil {
		var dir *os.File
		dir, err = os.Open(path)
		if err == nil {
			_, readErr := dir.Readdirnames(1)
			_ = dir.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				err = readErr
			}
		}
	}
	if err == nil {
		if sample, sampleErr := s.db.SourceHealthSampleForRoots(ctx, []string{rel}); sampleErr == nil {
			samplePath, pathErr := s.store.PhotoPath(sample)
			if pathErr == nil {
				file, openErr := os.Open(samplePath)
				if openErr == nil {
					var one [1]byte
					_, openErr = file.Read(one[:])
					_ = file.Close()
				}
				if openErr != nil && !errors.Is(openErr, os.ErrNotExist) && !errors.Is(openErr, io.EOF) {
					err = openErr
				}
			}
		}
	}
	if err != nil {
		status.Available = false
		status.Message = err.Error()
	}
	return status
}

func (s *Server) runAIHealthTask(w http.ResponseWriter, r *http.Request, action string) {
	switch action {
	case "check":
		go aiworker.RunHealthCheck(context.Background(), s.db, s.cfg.AIURL, s.logger)
	case "restart":
		go func() {
			_ = s.db.BeginSystemTask(context.Background(), db.SystemTaskAIHealth)
			err := aiworker.RestartService(context.Background(), s.cfg.AIURL)
			if err != nil {
				_ = s.db.FinishSystemTask(context.Background(), db.SystemTaskAIHealth, "failed", err.Error())
				return
			}
			_ = s.db.FinishSystemTask(context.Background(), db.SystemTaskAIHealth, "success", "AI 服务已重启并恢复")
		}()
	default:
		systemTaskActionError(w)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": "running"})
}

func (s *Server) systemTaskRoots(w http.ResponseWriter, r *http.Request, libraryID *string) ([]string, string, bool) {
	libraries, _, err := s.db.GetScanLibraries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "libraries_failed", "读取图库失败")
		return nil, "", false
	}
	if libraryID != nil && strings.TrimSpace(*libraryID) != "" {
		for _, library := range libraries {
			if library.ID == strings.TrimSpace(*libraryID) {
				return append([]string(nil), library.Roots...), library.Name, true
			}
		}
		writeError(w, http.StatusNotFound, "library_not_found", "图库不存在")
		return nil, "", false
	}
	var roots []string
	for _, library := range libraries {
		roots = append(roots, library.Roots...)
	}
	if len(roots) == 0 {
		roots = s.store.RootRelPaths()
	}
	return roots, "全部图库", true
}

func systemTaskActionError(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid_task_action", "任务操作无效")
}

func (s *Server) startOrphanCacheCleanup() bool {
	s.cleanupMu.Lock()
	if s.cleanupStatus.Running {
		s.cleanupMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cleanupStatus = CleanupStatusDTO{Running: true, Status: "running", UpdatedAt: time.Now().Unix()}
	s.cleanupCancel = cancel
	s.cleanupMu.Unlock()
	go s.runOrphanCacheCleanup(ctx)
	return true
}

func (s *Server) runOrphanCacheCleanup(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
	defer cancel()
	_ = s.db.BeginSystemTask(ctx, taskCacheCleanup)
	result, err := s.cachePolicy.Clear(ctx)
	deleted, bytes := result.DeletedFiles, result.ReleasedBytes
	status, message := "success", fmt.Sprintf("已删除 %d 个缓存项，释放 %d 字节", deleted, bytes)
	if errors.Is(err, context.Canceled) {
		status, message = "stopped", fmt.Sprintf("清理已停止；本次已删除 %d 个缓存项", deleted)
	} else if err != nil {
		status, message = "failed", "清理缓存失败"
	}
	_ = s.db.FinishSystemTask(context.Background(), taskCacheCleanup, status, message)
	s.cleanupMu.Lock()
	cleanupStatus := "done"
	lastError := ""
	if status == "stopped" {
		cleanupStatus = "stopped"
	} else if status == "failed" {
		cleanupStatus, lastError = "error", message
	}
	s.cleanupStatus = CleanupStatusDTO{Running: false, Status: cleanupStatus, LastError: lastError, UpdatedAt: time.Now().Unix()}
	s.cleanupCancel = nil
	s.cleanupMu.Unlock()
	s.cacheMu.Lock()
	s.cacheStatsAt = time.Time{}
	s.cacheMu.Unlock()
}

func cacheFileReferenced(base string, keys map[string]struct{}) bool {
	if len(base) < 20 {
		return false
	}
	key := base[:20]
	if _, ok := keys[key]; !ok {
		return false
	}
	return base == key || strings.HasPrefix(base, key+"-")
}

func (s *Server) startCacheCleanupScheduler() {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(next))
			s.startScheduledCacheMaintenance()
		}
	}()
}

func (s *Server) startScheduledCacheMaintenance() bool {
	s.cleanupMu.Lock()
	if s.cleanupStatus.Running {
		s.cleanupMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cleanupStatus = CleanupStatusDTO{Running: true, Status: "running", UpdatedAt: time.Now().Unix()}
	s.cleanupCancel = cancel
	s.cleanupMu.Unlock()
	go s.runScheduledCacheMaintenance(ctx)
	return true
}

func (s *Server) runScheduledCacheMaintenance(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
	defer cancel()
	_ = s.db.BeginSystemTask(ctx, taskCacheCleanup)
	before := s.cachePolicy.Usage()
	partial, err := s.cachePolicy.CleanupAbandoned(ctx, time.Minute)
	if err == nil && s.aiStager != nil {
		err = s.aiStager.CleanupAbandoned(ctx)
	}
	if err == nil {
		_, err = s.cachePolicy.EnsureCapacity(ctx, 0)
	}
	after := s.cachePolicy.Usage()
	released := max(int64(0), before.TotalBytes-after.TotalBytes)
	status, message := "success", fmt.Sprintf("已完成缓存维护，释放 %d 字节", released)
	if partial.DeletedFiles > 0 {
		message = fmt.Sprintf("已清除 %d 个中断残留，释放 %d 字节", partial.DeletedFiles, released)
	}
	if errors.Is(err, context.Canceled) {
		status, message = "stopped", "缓存维护已停止"
	} else if err != nil {
		status, message = "failed", err.Error()
	}
	_ = s.db.FinishSystemTask(context.Background(), taskCacheCleanup, status, message)
	s.cleanupMu.Lock()
	cleanupStatus := "done"
	lastError := ""
	if status == "stopped" {
		cleanupStatus = "stopped"
	} else if status == "failed" {
		cleanupStatus, lastError = "error", message
	}
	s.cleanupStatus = CleanupStatusDTO{Running: false, Status: cleanupStatus, LastError: lastError, UpdatedAt: time.Now().Unix()}
	s.cleanupCancel = nil
	s.cleanupMu.Unlock()
	s.cacheMu.Lock()
	s.cacheStatsAt = time.Time{}
	s.cacheMu.Unlock()
}

func (s *Server) startStorageHealthScheduler() {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(next))
			_, failed, _ := s.checkStorageHealth(context.Background())
			if failed == 0 {
				s.scanner.RequestReconcileScan("daily_source_window")
			}
		}
	}()
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	if systemTaskNeedsStorage(taskID) {
		for _, root := range roots {
			if status := s.probeConfiguredLibraryRoot(r.Context(), libraryName, root); !status.Available {
				writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "存储不可访问，任务没有启动")
				return
			}
		}
	}
	if action != "stop" && action != "check" && action != "restart" && s.systemTaskRunning(r.Context(), taskID) {
		writeJSON(w, http.StatusConflict, map[string]any{"accepted": false, "state": "running", "message": "任务正在运行，请先停止当前任务"})
		return
	}

	switch taskID {
	case "library_scan":
		if action != "scan" {
			systemTaskActionError(w)
			return
		}
		result := s.scanner.RequestCountScanRoots(reason, roots)
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "metadata_extraction":
		s.runMetadataTask(w, r, action, roots, reason)
	case "thumbnail_creation":
		s.runThumbnailTask(w, r, action, roots, reason)
	case "preview_creation", "video_poster_creation":
		taskType := "preview"
		if taskID == "video_poster_creation" {
			taskType = "video_poster"
		}
		s.runQueuedMediaTask(w, r, taskType, action, roots)
	case "ai_analysis":
		s.runAIAnalysisTask(w, r, action)
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
	if taskID == "library_scan" || taskID == "metadata_extraction" || taskID == "thumbnail_creation" {
		status, _ := s.scanner.Status(ctx)
		if status.Running {
			return true
		}
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
	case "ai_analysis":
		return stats.AIQueued+stats.ActiveAI > 0
	default:
		return false
	}
}

func systemTaskNeedsStorage(taskID string) bool {
	switch taskID {
	case "library_scan", "metadata_extraction", "thumbnail_creation", "preview_creation", "video_poster_creation", "ai_analysis":
		return true
	default:
		return false
	}
}

func (s *Server) stopSystemTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(chi.URLParam(r, "id"))
	ctx := r.Context()
	switch taskID {
	case "library_scan", "metadata_extraction":
		result := s.scanner.RequestStop()
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "thumbnail_creation":
		_ = s.jobs.ClearQueues(ctx, "thumb", "video_poster")
		_ = s.db.RequeueProcessingWork(ctx, "thumb")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
		result := s.scanner.RequestStop()
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusAccepted, scanCommandResponse(result))
	case "preview_creation":
		_ = s.jobs.ClearQueues(ctx, "preview")
		_ = s.db.RequeueProcessingWork(ctx, "preview")
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case "video_poster_creation":
		_ = s.jobs.ClearQueues(ctx, "video_poster")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "任务已停止，可从当前进度继续")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
	case "ai_analysis":
		_, _ = s.db.SetAIAutoAnalyze(ctx, false)
		_, _ = s.db.SetAIManualRun(ctx, false)
		if err := s.jobs.ClearAIQueue(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_stop_failed", "停止 AI 分析失败")
			return
		}
		_ = s.db.FinishSystemTask(ctx, taskID, "stopped", "AI 分析已停止；自动分析同时关闭")
		writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "state": "paused"})
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

func (s *Server) runMetadataTask(w http.ResponseWriter, r *http.Request, action string, roots []string, reason string) {
	switch action {
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
		result := s.scanner.RequestMetadataScanPaths(reason, roots, paths)
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": result.Accepted, "count": len(paths), "state": result.State})
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

func (s *Server) runThumbnailTask(w http.ResponseWriter, r *http.Request, action string, roots []string, reason string) {
	_ = s.db.BeginSystemTask(context.Background(), "thumbnail_creation")
	var result any
	switch action {
	case "continue":
		result = scanCommandResponse(s.scanner.RequestThumbnailContinueRoots(reason, roots))
	case "retry_failed":
		items, err := s.db.RetryFailedWorkForRoots(r.Context(), "thumb", roots)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "thumbnail_retry_failed", "重试缩略图失败任务失败")
			return
		}
		for _, item := range items {
			s.jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
		}
		result = map[string]any{"accepted": true, "count": len(items), "state": "queued"}
	case "rebuild":
		result = scanCommandResponse(s.scanner.RequestThumbnailRebuildRoots(reason, roots))
	default:
		systemTaskActionError(w)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) runQueuedMediaTask(w http.ResponseWriter, r *http.Request, taskType string, action string, roots []string) {
	if action != "continue" && action != "retry_failed" && action != "rebuild" {
		systemTaskActionError(w)
		return
	}
	taskID := "preview_creation"
	if taskType == "video_poster" {
		taskID = "video_poster_creation"
	}
	_ = s.db.BeginSystemTask(r.Context(), taskID)
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
	if err != nil {
		writeError(w, http.StatusInternalServerError, "task_work_failed", "读取待处理媒体失败")
		return
	}
	for _, item := range items {
		s.jobs.Enqueue(jobs.Task{Type: item.Type, AssetID: item.AssetID})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(items), "state": "queued"})
}

func (s *Server) runAIAnalysisTask(w http.ResponseWriter, r *http.Request, action string) {
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
			if s.jobs != nil {
				s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 1})
			}
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
			if s.jobs != nil {
				s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 100})
			}
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(queued), "settings": settings})
	case "retry_failed", "reset_failed":
		items, err := s.db.RetryFailedAI(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_retry_failed", "重置 AI 失败任务失败")
			return
		}
		if len(items) > 0 {
			_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
		}
		for _, item := range items {
			s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 1})
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(items)})
	case "rebuild":
		count, err := s.db.ReindexAI(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_rebuild_failed", "重建 AI 分析任务失败")
			return
		}
		_ = s.db.BeginSystemTask(r.Context(), "ai_analysis")
		_, _ = s.db.SetAIManualRun(r.Context(), true)
		queued, err := s.enqueueAIBackfillNow(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": count, "queued": queued})
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
		_ = s.jobs.ClearQueues(ctx, "thumb", "preview", "video_poster")
		_ = s.jobs.ClearAIQueue(ctx)
		_ = s.db.RequeueProcessingWork(ctx, "thumb")
		_ = s.db.RequeueProcessingWork(ctx, "preview")
		_ = s.db.RequeueProcessingWork(ctx, "video_poster")
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
	keys, err := s.db.AssetCacheKeys(ctx)
	deleted, bytes := 0, int64(0)
	if err == nil {
		err = filepath.WalkDir(s.store.CacheRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			base := strings.SplitN(entry.Name(), ".", 2)[0]
			preserve := cacheFileReferenced(base, keys)
			if preserve || strings.Contains(entry.Name(), ".tmp") && time.Since(info.ModTime()) < 24*time.Hour {
				return nil
			}
			if removeErr := os.Remove(path); removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				deleted++
				bytes += info.Size()
			}
			return nil
		})
	}
	status, message := "success", fmt.Sprintf("已删除 %d 个无效缓存文件，释放 %d 字节", deleted, bytes)
	if errors.Is(err, context.Canceled) {
		status, message = "stopped", fmt.Sprintf("清理已停止；本次已删除 %d 个无效缓存文件", deleted)
	} else if err != nil {
		status, message = "failed", "清理无效缓存失败"
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
			s.startOrphanCacheCleanup()
		}
	}()
}

func (s *Server) startStorageHealthScheduler() {
	go func() {
		s.checkStorageHealth(context.Background())
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.checkStorageHealth(context.Background())
		}
	}()
}

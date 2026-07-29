package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lpicto/backend/internal/db"
)

const mediaLibraryResetConfirmation = "彻底重置"

type mediaLibraryResetRequest struct {
	Confirmation string `json:"confirmation"`
}

type mediaLibraryResetResponse struct {
	Reset         bool  `json:"reset"`
	DeletedAssets int64 `json:"deletedAssets"`
	DeletedFiles  int64 `json:"deletedFiles"`
	ReleasedBytes int64 `json:"releasedBytes"`
}

func (s *Server) resetMediaLibrary(w http.ResponseWriter, r *http.Request) {
	var payload mediaLibraryResetRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&payload) != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	if strings.TrimSpace(payload.Confirmation) != mediaLibraryResetConfirmation {
		writeError(w, http.StatusBadRequest, "confirmation_required", "请输入“彻底重置”确认")
		return
	}
	s.mediaResetMu.Lock()
	if s.mediaResetting {
		s.mediaResetMu.Unlock()
		writeError(w, http.StatusConflict, "media_library_reset_running", "媒体库正在重置")
		return
	}
	s.mediaResetting = true
	s.mediaResetMu.Unlock()
	defer func() {
		s.mediaResetMu.Lock()
		s.mediaResetting = false
		s.mediaResetMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	aiSettings, _ := s.db.GetAISettings(ctx)
	_, _ = s.db.SetAIAutoAnalyze(ctx, false)
	_, _ = s.db.SetAIManualRun(ctx, false)
	defer func() {
		if aiSettings.AutoAnalyze {
			_, _ = s.db.SetAIAutoAnalyze(context.Background(), true)
		}
	}()

	if s.scanner != nil {
		s.scanner.RequestStop()
	}
	s.stopCacheCleanup()
	if s.jobs != nil {
		if err := s.jobs.ClearAllQueues(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "media_library_queue_stop_failed", "停止后台队列失败")
			return
		}
	}
	s.pauseAIService()
	s.stopAllVideoWork()
	if !s.waitForMediaWorkToStop(ctx, 10*time.Second) {
		writeError(w, http.StatusConflict, "media_library_tasks_busy", "后台任务尚未停止，请稍后重试")
		return
	}
	if s.jobs != nil {
		_ = s.jobs.ClearAllQueues(ctx)
		s.jobs.ResetRuntimeState(ctx)
	}
	reset, err := s.db.ResetMediaLibrary(ctx)
	if err != nil {
		s.logger.Error("reset media library database failed", "error", err)
		writeError(w, http.StatusInternalServerError, "media_library_database_reset_failed", "清空媒体数据库失败")
		return
	}
	files, bytes, err := clearDirectoryContents(s.store.CacheRoot)
	if err != nil {
		s.logger.Error("reset media library cache failed", "error", err)
		writeError(w, http.StatusInternalServerError, "media_library_cache_reset_failed", "媒体数据库已清空，但缓存文件清理失败")
		return
	}
	for _, dir := range []string{"thumbs", "previews", "video-posters", "video-proxies", "originals", "video-chunks", "ai-staging"} {
		if err := os.MkdirAll(filepath.Join(s.store.CacheRoot, dir), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "media_library_cache_prepare_failed", "重新创建缓存目录失败")
			return
		}
	}
	s.invalidateMediaLibraryCaches()
	writeJSON(w, http.StatusOK, mediaLibraryResetResponse{
		Reset: true, DeletedAssets: reset.DeletedAssets, DeletedFiles: files, ReleasedBytes: bytes,
	})
}

func (s *Server) waitForMediaWorkToStop(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		activeWork := false
		if s.jobs != nil {
			stats := s.jobs.Stats()
			activeWork = stats.ActiveThumb > 0 || stats.ActivePreview > 0 || stats.ActiveVideoPoster > 0 || stats.ActiveAI > 0
		}
		scanRunning := false
		if s.scanner != nil {
			status, err := s.scanner.Status(ctx)
			if err == nil {
				scanRunning = status.Running
			}
		}
		s.cleanupMu.Lock()
		cleanupRunning := s.cleanupStatus.Running
		s.cleanupMu.Unlock()
		if !scanRunning && !activeWork && !cleanupRunning {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func (s *Server) stopCacheCleanup() {
	s.cleanupMu.Lock()
	cancel := s.cleanupCancel
	s.cleanupMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) pauseAIService() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.AIURL, "/")+"/pause", nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		s.logger.Warn("pause AI service for media library reset failed", "error", err)
		return
	}
	response.Body.Close()
}

func (s *Server) stopAllVideoWork() {
	var done []<-chan struct{}
	s.videoProxyMu.Lock()
	for _, state := range s.videoProxyStates {
		if state == nil {
			continue
		}
		if state.Cancel != nil {
			state.Cancel()
		}
		if state.Done != nil && (state.Queued || state.Transcoding) {
			done = append(done, state.Done)
		}
	}
	for _, state := range s.videoSegmentStates {
		if state == nil {
			continue
		}
		if state.Cancel != nil {
			state.Cancel(errVideoSegmentSessionStop)
		}
		if state.Done != nil && (state.Queued || state.Claiming || state.Transcoding) {
			done = append(done, state.Done)
		}
	}
	s.videoProxyMu.Unlock()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for _, finished := range done {
		select {
		case <-finished:
		case <-timer.C:
			return
		}
	}
	s.videoProxyMu.Lock()
	s.videoProxyStates = map[string]*videoProxyRuntime{}
	s.videoSegmentStates = map[string]*videoSegmentRuntime{}
	s.videoProxyMu.Unlock()
}

func clearDirectoryContents(root string) (int64, int64, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return 0, 0, err
	}
	volumeRoot := filepath.VolumeName(absolute) + string(filepath.Separator)
	if absolute == volumeRoot || absolute == "." {
		return 0, 0, errors.New("refusing to clear filesystem root")
	}
	entries, err := os.ReadDir(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, os.MkdirAll(absolute, 0o755)
	}
	if err != nil {
		return 0, 0, err
	}
	var files int64
	var bytes int64
	for _, entry := range entries {
		target := filepath.Join(absolute, entry.Name())
		err = filepath.WalkDir(target, func(_ string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() {
				return nil
			}
			info, infoErr := item.Info()
			if infoErr != nil {
				return infoErr
			}
			files++
			bytes += info.Size()
			return nil
		})
		if err != nil {
			return files, bytes, err
		}
		if err = os.RemoveAll(target); err != nil {
			return files, bytes, err
		}
	}
	return files, bytes, nil
}

func (s *Server) invalidateMediaLibraryCaches() {
	s.progressMu.Lock()
	s.progressStats = db.ProcessingProgress{}
	s.progressStatsAt = time.Time{}
	s.progressRefreshing = false
	s.progressMu.Unlock()
	s.cacheMu.Lock()
	s.cacheStats = CacheStatsDTO{}
	s.cacheStatsAt = time.Time{}
	s.cacheRefreshing = false
	s.cacheMu.Unlock()
	s.libraryCountsMu.Lock()
	s.libraryCountsKey = ""
	s.libraryCounts = nil
	s.libraryCountsAt = time.Time{}
	s.libraryCountsRefreshing = false
	s.libraryCountsMu.Unlock()
	s.sourceDirMu.Lock()
	s.sourceDirCache = map[string]sourceDirCacheEntry{}
	s.sourceDirMu.Unlock()
}

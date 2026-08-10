package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	"lpicto/backend/internal/thumb"
)

type storyboardDTO struct {
	AssetID    int64   `json:"assetId"`
	CacheKey   string  `json:"cacheKey"`
	FrameCount int     `json:"frameCount"`
	SheetCount int     `json:"sheetCount"`
	Columns    int     `json:"columns"`
	Rows       int     `json:"rows"`
	CellWidth  int     `json:"cellWidth"`
	CellHeight int     `json:"cellHeight"`
	Interval   float64 `json:"interval"`
}

func (s *Server) generateAssetStoryboard(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo || asset.Duration == nil {
		writeError(w, http.StatusConflict, "storyboard_not_applicable", "该媒体不能生成进度预览图")
		return
	}
	_, sheetCount, _ := thumb.StoryboardLayout(*asset.Duration)
	if sheetCount == 0 {
		writeError(w, http.StatusConflict, "storyboard_duration_missing", "缺少视频时长，请先提取媒体信息")
		return
	}
	ready := true
	for index := 0; index < sheetCount; index++ {
		path, err := s.store.CacheFilePath("storyboards", thumb.StoryboardSheetKey(asset.CacheKey, index), "webp")
		if err != nil {
			ready = false
			break
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			ready = false
			break
		}
	}
	if ready {
		_ = s.db.SetStoryboardJobStatus(r.Context(), asset.ID, model.StatusReady, nil)
		writeJSON(w, http.StatusOK, map[string]any{"accepted": false, "state": model.StatusReady})
		return
	}
	status, err := s.db.StoryboardJobStatus(r.Context(), asset.ID)
	if err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "storyboard_queue_failed", "进度预览图加入队列失败")
		return
	}
	if err == sql.ErrNoRows || status == model.StatusReady || status == model.StatusError || status == model.StatusNotRequired {
		if err := s.db.SetStoryboardJobStatus(r.Context(), asset.ID, model.StatusPending, nil); err != nil {
			writeError(w, http.StatusInternalServerError, "storyboard_queue_failed", "进度预览图加入队列失败")
			return
		}
		status = model.StatusPending
	}
	s.jobs.Enqueue(jobs.Task{Type: "storyboard", AssetID: asset.ID, Reason: "viewer", Priority: 10})
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "state": status})
}

func (s *Server) assetStoryboard(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo || asset.Duration == nil {
		writeError(w, http.StatusNotFound, "storyboard_not_ready", "进度预览图尚未生成")
		return
	}
	frameCount, sheetCount, interval := thumb.StoryboardLayout(*asset.Duration)
	if sheetCount == 0 {
		writeError(w, http.StatusNotFound, "storyboard_not_ready", "进度预览图尚未生成")
		return
	}
	for index := 0; index < sheetCount; index++ {
		path, err := s.store.CacheFilePath("storyboards", thumb.StoryboardSheetKey(asset.CacheKey, index), "webp")
		if err != nil {
			writeError(w, http.StatusNotFound, "storyboard_not_ready", "进度预览图尚未生成")
			return
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "storyboard_not_ready", "进度预览图尚未生成")
			return
		}
	}
	writeJSON(w, http.StatusOK, storyboardDTO{
		AssetID: asset.ID, CacheKey: asset.CacheKey, FrameCount: frameCount, SheetCount: sheetCount,
		Columns: 4, Rows: 4, CellWidth: 160, CellHeight: 90, Interval: interval,
	})
}

func (s *Server) assetStoryboardSheet(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(chi.URLParam(r, "sheet"))
	if err != nil || index < 0 || asset.Duration == nil {
		http.NotFound(w, r)
		return
	}
	_, sheetCount, _ := thumb.StoryboardLayout(*asset.Duration)
	if index >= sheetCount {
		http.NotFound(w, r)
		return
	}
	path, err := s.store.CacheFilePath("storyboards", thumb.StoryboardSheetKey(asset.CacheKey, index), "webp")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	release := s.cachePolicy.Pin(path)
	defer release()
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d"`, asset.CacheKey, index))
	s.cachePolicy.Touch(r.Context(), "storyboards", asset.CacheKey, path)
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

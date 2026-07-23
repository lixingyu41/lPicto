package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lpicto/backend/internal/jobs"
)

type aiSettingsRequest struct {
	AutoAnalyze bool `json:"autoAnalyze"`
}

func (s *Server) assetAI(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	result, err := s.db.GetAIResult(r.Context(), asset.ID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"assetId": asset.ID, "inputCacheKey": asset.CacheKey, "status": "pending", "description": "", "tags": []any{}, "palette": []any{}, "attempts": 0, "sampledFrames": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_ai_failed", "读取 AI 结果失败")
		return
	}
	if result.InputCacheKey != asset.CacheKey {
		result.Status = "pending"
		result.Description = ""
		result.Tags = nil
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) reanalyzeAssetAI(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if err := s.db.EnsureAIQueued(r.Context(), asset.ID, asset.CacheKey, true); err != nil {
		writeError(w, http.StatusInternalServerError, "asset_ai_reanalyze_failed", "重新分析入队失败")
		return
	}
	_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
	s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: asset.ID, Priority: 1})
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "assetId": asset.ID})
}

func (s *Server) aiStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.db.AIStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_status_failed", "读取 AI 状态失败")
		return
	}
	stats := s.jobs.Stats()
	status.Queued = stats.AIQueued
	status.Active = stats.ActiveAI
	remaining := status.Total - status.Ready
	if status.PerMinute > 0 && remaining > 0 {
		eta := int64(float64(remaining) / status.PerMinute * 60)
		status.ETASeconds = &eta
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) aiSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.db.GetAISettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_settings_failed", "读取 AI 设置失败")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) updateAISettings(w http.ResponseWriter, r *http.Request) {
	var payload aiSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	settings, err := s.db.SetAIAutoAnalyze(r.Context(), payload.AutoAnalyze)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_settings_update_failed", "保存 AI 设置失败")
		return
	}
	if payload.AutoAnalyze {
		_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
		if _, err := s.enqueueAIBackfillNow(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动 AI 分析失败")
			return
		}
	} else if s.jobs != nil {
		if err := s.jobs.ClearAIQueue(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_queue_clear_failed", "停止 AI 队列失败")
			return
		}
		_ = s.db.FinishSystemTask(r.Context(), "ai_analysis", "stopped", "AI 自动分析已停止")
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) runAIManually(w http.ResponseWriter, r *http.Request) {
	_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
	settings, err := s.db.SetAIManualRun(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_manual_run_failed", "启动手动 AI 分析失败")
		return
	}
	count, err := s.enqueueAIBackfillNow(r.Context())
	if err != nil {
		_, _ = s.db.SetAIManualRun(r.Context(), false)
		writeError(w, http.StatusInternalServerError, "ai_enqueue_failed", "启动手动 AI 分析失败")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": count, "settings": settings})
}

func (s *Server) stopAIManually(w http.ResponseWriter, r *http.Request) {
	settings, err := s.db.SetAIManualRun(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_manual_stop_failed", "停止手动 AI 分析失败")
		return
	}
	if s.jobs != nil {
		if err := s.jobs.ClearAIQueue(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "ai_queue_clear_failed", "停止 AI 队列失败")
			return
		}
	}
	_ = s.db.FinishSystemTask(r.Context(), "ai_analysis", "stopped", "AI 手动分析已停止")
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) enqueueAIBackfillNow(ctx context.Context) (int, error) {
	items, err := s.db.AIBackfillBatch(ctx, 1000)
	if err != nil {
		return 0, err
	}
	if len(items) > 0 {
		_ = s.db.EnsureSystemTaskRunning(ctx, "ai_analysis")
	}
	for _, item := range items {
		if err := s.db.EnsureAIQueued(ctx, item.AssetID, item.CacheKey, false); err != nil {
			return 0, err
		}
		if s.jobs != nil {
			s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 100})
		}
	}
	return len(items), nil
}

func (s *Server) reindexAI(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.ReindexAI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_reindex_failed", "AI 重建索引失败")
		return
	}
	_ = s.db.BeginSystemTask(r.Context(), "ai_analysis")
	_, _ = s.db.SetAIManualRun(r.Context(), true)
	_, _ = s.enqueueAIBackfillNow(r.Context())
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": count})
}

func (s *Server) retryFailedAI(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.RetryFailedAI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_retry_failed", "重试 AI 失败任务失败")
		return
	}
	if len(items) > 0 {
		_ = s.db.EnsureSystemTaskRunning(r.Context(), "ai_analysis")
	}
	for _, item := range items {
		if s.jobs != nil {
			s.jobs.Enqueue(jobs.Task{Type: "ai_analyze", AssetID: item.AssetID, Priority: 1})
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "count": len(items)})
}

func (s *Server) aiTags(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.AITags(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_tags_failed", "读取 AI 标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

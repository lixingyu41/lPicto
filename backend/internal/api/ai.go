package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
)

type aiSettingsRequest struct {
	AutoAnalyze bool `json:"autoAnalyze"`
}

type aiTagMutationRequest struct {
	PreviousTag string `json:"previousTag"`
	Tag         string `json:"tag"`
	CategoryKey string `json:"categoryKey"`
	SubjectKey  string `json:"subjectKey"`
}

var editableAITagSubjects = map[string]map[string]string{
	"people": {"count": "人数"},
	"action": {"posture": "姿态", "activity": "动作"},
	"shoes":  {"shoes": "鞋子"},
	"socks":  {"socks": "袜子"},
	"clothes": {
		"top": "上衣", "outerwear": "外套", "dress": "裙装", "pants": "裤装",
		"sportswear": "运动服", "swimwear": "泳装", "hat": "帽子", "accessories": "配饰",
	},
	"closeup": {"part": "部位"},
}

var editableAITagCategoryLabels = map[string]string{
	"people": "人物", "action": "动作", "shoes": "鞋子", "socks": "袜子",
	"clothes": "衣服", "closeup": "特写",
}

var editableCloseupParts = map[string]struct{}{
	"脸部": {}, "头部": {}, "眼部": {}, "鼻部": {}, "嘴部": {}, "嘴唇": {}, "舌部": {}, "牙齿": {}, "耳部": {},
	"颈部": {}, "肩部": {}, "锁骨": {}, "胸部": {}, "腹部": {}, "肚脐": {}, "腰部": {}, "背部": {},
	"手部": {}, "手掌": {}, "手指": {}, "手臂": {}, "肘部": {}, "手腕": {},
	"臀部": {}, "腿部": {}, "大腿": {}, "膝部": {}, "小腿": {}, "脚踝": {}, "脚部": {}, "脚底": {}, "脚趾": {}, "全身": {},
}

var editablePeopleCountPattern = regexp.MustCompile(`^[1-9][0-9]*人$`)

func (s *Server) assetAI(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType == model.MediaTypeAudio {
		writeJSON(w, http.StatusOK, map[string]any{"assetId": asset.ID, "inputCacheKey": asset.CacheKey, "status": "not_required", "description": "", "tags": []any{}, "palette": []any{}, "attempts": 0, "sampledFrames": []any{}})
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
		result.Tags = []db.AITag{}
		result.Palette = []db.AIColor{}
		result.SampledFrames = json.RawMessage(`[]`)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) reanalyzeAssetAI(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType == model.MediaTypeAudio {
		writeError(w, http.StatusBadRequest, "audio_ai_not_supported", "音频不参与媒体 AI 分析")
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

func (s *Server) replaceAssetAITag(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	var payload aiTagMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	tag, err := editableAITag(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "asset_ai_tag_invalid", err.Error())
		return
	}
	result, err := s.db.ReplaceAITag(r.Context(), asset.ID, asset.CacheKey, payload.PreviousTag, tag)
	if errors.Is(err, db.ErrAITagLimit) {
		writeError(w, http.StatusConflict, "asset_ai_tag_limit", "AI 标签最多 10 个")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "asset_ai_not_ready", "当前媒体还没有可编辑的 AI 结果")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_ai_tag_update_failed", "保存 AI 标签失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteAssetAITag(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	result, err := s.db.DeleteAITag(r.Context(), asset.ID, r.URL.Query().Get("tag"))
	if errors.Is(err, db.ErrEmptyAITag) {
		writeError(w, http.StatusBadRequest, "asset_ai_tag_empty", "标签不能为空")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusConflict, "asset_ai_not_ready", "当前媒体还没有可编辑的 AI 结果")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_ai_tag_delete_failed", "删除 AI 标签失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func editableAITag(payload aiTagMutationRequest) (db.AITag, error) {
	tag := strings.TrimSpace(payload.Tag)
	categoryKey := strings.TrimSpace(payload.CategoryKey)
	subjectKey := strings.TrimSpace(payload.SubjectKey)
	subjects, ok := editableAITagSubjects[categoryKey]
	if !ok {
		return db.AITag{}, errors.New("标签分类无效")
	}
	subjectLabel, ok := subjects[subjectKey]
	if !ok {
		return db.AITag{}, errors.New("标签类型无效")
	}
	if categoryKey == "closeup" {
		part := strings.TrimSuffix(tag, "特写")
		if _, ok = editableCloseupParts[part]; !ok {
			return db.AITag{}, errors.New("请选择有效的特写部位")
		}
		tag = part + "特写"
	} else if categoryKey == "people" && subjectKey == "count" && !editablePeopleCountPattern.MatchString(tag) {
		return db.AITag{}, errors.New("人数必须使用“N人”格式")
	}
	if tag == "" || len([]rune(tag)) > 80 || strings.Contains(tag, "无法判断") {
		return db.AITag{}, errors.New("标签内容无效")
	}
	categoryLabel := editableAITagCategoryLabels[categoryKey]
	value := strings.TrimSuffix(tag, "特写")
	dimensionKey, dimensionLabel := "type", "类型"
	if categoryKey == "people" {
		dimensionKey, dimensionLabel, value = "count", "人数", tag
	} else if categoryKey == "action" {
		dimensionKey, dimensionLabel = subjectKey, subjectLabel
	}
	facetKey := categoryKey + "." + subjectKey + "." + dimensionKey
	nodeIDs := []string{
		"ai:" + categoryKey,
		"ai:" + categoryKey + "." + subjectKey,
		"ai:" + facetKey,
		"ai:" + facetKey + ":" + value,
	}
	return db.AITag{
		Tag: tag, Confidence: 1,
		CategoryKey: categoryKey, CategoryLabel: categoryLabel,
		SubjectKey: subjectKey, SubjectLabel: subjectLabel,
		Facets: []db.AITagFacet{{
			FacetKey: facetKey,
			NodeID:   nodeIDs[len(nodeIDs)-1],
			NodeIDs:  nodeIDs,
			Labels:   []string{categoryLabel, subjectLabel, dimensionLabel, value},
		}},
	}, nil
}

func (s *Server) aiStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.db.AIStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_status_failed", "读取 AI 状态失败")
		return
	}
	stats := jobs.QueueStats{}
	if s.jobs != nil {
		stats = s.jobs.Stats()
	}
	status.Queued = stats.AIQueued
	status.Active = stats.ActiveAI
	status.Staged, status.StagedBytes, _ = s.db.AIStageStats(r.Context())
	if batch, batchErr := s.db.LatestSourceIOBatch(r.Context()); batchErr == nil && batch != nil && batch.State == "running" {
		status.SourceReading = true
	}
	settings, _ := s.db.GetAISettings(r.Context())
	status.PausedReason = s.aiPauseReason(r.Context(), status, settings)
	remaining := status.Total - status.Ready
	if status.PerMinute > 0 && remaining > 0 {
		eta := int64(float64(remaining) / status.PerMinute * 60)
		status.ETASeconds = &eta
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) aiPauseReason(ctx context.Context, status db.AIStatus, settings db.AISettings) string {
	remaining := status.Pending + status.Stale + status.Processing + status.Failed
	if remaining == 0 {
		return ""
	}
	if !settings.AutoAnalyze && !settings.ManualRun {
		return "AI 已停止，点击“继续分析”后恢复"
	}
	if s.jobs != nil {
		switch s.jobs.BackgroundBlocker(ctx) {
		case "playback", "foreground":
			return "正在播放或加载媒体，AI 暂时暂停"
		case "storyboard":
			return "正在创建视频进度预览图，AI 暂时暂停"
		case "load":
			return "系统负载较高，AI 暂时等待"
		case "memory":
			return "可用内存不足，AI 暂时等待"
		}
	}
	if status.Active == 0 && status.Queued > 0 {
		return "任务已排队，正在等待 AI 推理"
	}
	if status.Active == 0 && status.Pending+status.Stale > 0 {
		if status.SourceReading {
			return "正在集中读取媒体并准备 AI 输入"
		}
		return "正在准备下一批 AI 输入"
	}
	return ""
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
		s.removeQueuedAIStages(r.Context())
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
	s.removeQueuedAIStages(r.Context())
	_ = s.db.FinishSystemTask(r.Context(), "ai_analysis", "stopped", "AI 手动分析已停止")
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) removeQueuedAIStages(ctx context.Context) {
	if s.aiStager == nil {
		return
	}
	if err := s.aiStager.RemoveReady(ctx); err != nil && s.logger != nil {
		s.logger.Warn("remove queued AI staging failed", "error", err)
	}
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
	running := s.systemTaskRunning(r.Context(), "ai_analysis")
	items, err := s.db.RetryFailedAI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_retry_failed", "重试 AI 失败任务失败")
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
}

func (s *Server) aiTags(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.AITags(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_tags_failed", "读取 AI 标签失败")
		return
	}
	tree, err := s.db.AITagTree(r.Context(), tagNodesQuery(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_tag_tree_failed", "读取标签层级失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "tree": tree})
}

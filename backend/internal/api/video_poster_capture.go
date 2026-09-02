package api

import (
	"encoding/json"
	"math"
	"net/http"

	"lpicto/backend/internal/debugcontrol"
	"lpicto/backend/internal/model"
)

type captureVideoPosterRequest struct {
	TimeSeconds float64 `json:"timeSeconds"`
}

func (s *Server) captureVideoPoster(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	if debugcontrol.ExternalFileAccessPaused() {
		writeError(w, http.StatusServiceUnavailable, "external_access_paused", "调试模式已暂停外部文件访问")
		return
	}
	var payload captureVideoPosterRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	if payload.TimeSeconds < 0 || math.IsNaN(payload.TimeSeconds) || math.IsInf(payload.TimeSeconds, 0) {
		writeError(w, http.StatusBadRequest, "capture_time_invalid", "截图时间无效")
		return
	}
	updated, err := s.posterMaker.CaptureVideoPosterAt(r.Context(), asset.ID, payload.TimeSeconds)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("capture video poster failed", "assetID", asset.ID, "timeSeconds", payload.TimeSeconds, "error", err)
		}
		writeError(w, http.StatusInternalServerError, "poster_capture_failed", "设置视频封面失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset":       assetDTO(updated),
		"timeSeconds": payload.TimeSeconds,
	})
}

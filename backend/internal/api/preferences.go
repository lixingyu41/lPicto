package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"lpicto/backend/internal/db"
)

type assetPreferenceRequest struct {
	Rotation *int `json:"rotation"`
	Rating   *int `json:"rating"`
}

func (s *Server) assetPreferences(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	pref, err := s.db.GetAssetPreference(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_preferences_failed", "读取资源设置失败")
		return
	}
	writeJSON(w, http.StatusOK, assetPreferenceDTO(pref))
}

func (s *Server) updateAssetPreferences(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	var payload assetPreferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	if payload.Rotation == nil && payload.Rating == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少资源设置")
		return
	}
	if payload.Rating != nil && !db.ValidRating(*payload.Rating) {
		writeError(w, http.StatusBadRequest, "rating_invalid", "星级必须是 0 到 5")
		return
	}
	pref, err := s.db.SetAssetPreferences(r.Context(), id, payload.Rotation, payload.Rating)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "asset_not_found", "资源不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_preferences_update_failed", "保存资源设置失败")
		return
	}
	writeJSON(w, http.StatusOK, assetPreferenceDTO(pref))
}

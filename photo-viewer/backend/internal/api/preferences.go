package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

type assetPreferenceRequest struct {
	Rotation int `json:"rotation"`
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
	pref, err := s.db.SetAssetRotation(r.Context(), id, payload.Rotation)
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

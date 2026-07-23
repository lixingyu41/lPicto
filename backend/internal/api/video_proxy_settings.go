package api

import (
	"encoding/json"
	"net/http"
	"time"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/db"
)

func (s *Server) videoProxySettings(w http.ResponseWriter, r *http.Request) {
	settings := s.videoProxyCacheSettings(r.Context())
	writeJSON(w, http.StatusOK, videoProxySettingsDTO(settings))
}

func (s *Server) updateVideoProxySettings(w http.ResponseWriter, r *http.Request) {
	var payload VideoProxySettingsDTO
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请求内容无效")
		return
	}
	normalized := db.VideoProxyCacheSettings{
		TTLSeconds: int64(config.NormalizeVideoProxyCacheTTL(time.Duration(payload.CacheTTLSeconds) * time.Second).Seconds()),
		MaxBytes:   config.NormalizeVideoProxyCacheMaxBytes(payload.MaxCacheBytes),
	}
	if err := s.db.SetVideoProxyCacheSettings(r.Context(), normalized); err != nil {
		writeError(w, http.StatusInternalServerError, "video_proxy_settings_failed", "保存转码缓存设置失败")
		return
	}
	go s.sweepVideoProxyCache()
	settings := s.videoProxyCacheSettings(r.Context())
	writeJSON(w, http.StatusOK, videoProxySettingsDTO(settings))
}

func videoProxySettingsDTO(settings videoProxyCacheSettings) VideoProxySettingsDTO {
	return VideoProxySettingsDTO{
		CacheTTLSeconds: int64(settings.TTL.Seconds()),
		MaxCacheBytes:   settings.MaxBytes,
	}
}

package api

import (
	"encoding/json"
	"net/http"

	"lpicto/backend/internal/db"
	"lpicto/backend/internal/debugcontrol"
)

type debugSettingsDTO struct {
	ExternalFileAccessPaused   bool `json:"externalFileAccessPaused"`
	BackgroundProcessingPaused bool `json:"backgroundProcessingPaused"`
	EffectiveBackgroundPaused  bool `json:"effectiveBackgroundPaused"`
}

func (s *Server) debugSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.db.GetDebugSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "debug_settings_failed", "读取调试设置失败")
		return
	}
	writeJSON(w, http.StatusOK, debugSettingsResponse(settings))
}

func (s *Server) updateDebugSettings(w http.ResponseWriter, r *http.Request) {
	var payload db.DebugSettings
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_debug_settings", "调试设置格式无效")
		return
	}
	previous, err := s.db.GetDebugSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "debug_settings_failed", "读取调试设置失败")
		return
	}
	if err := s.db.SetDebugSettings(r.Context(), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "debug_settings_failed", "保存调试设置失败")
		return
	}
	debugcontrol.Apply(payload.ExternalFileAccessPaused, payload.BackgroundProcessingPaused)
	wasEffectivelyPaused := previous.ExternalFileAccessPaused || previous.BackgroundProcessingPaused
	isEffectivelyPaused := payload.ExternalFileAccessPaused || payload.BackgroundProcessingPaused
	if isEffectivelyPaused && !wasEffectivelyPaused {
		s.scanner.RequestStop()
		s.jobs.CancelActive("thumb", "preview", "video_poster", "storyboard", "ai_analyze")
		s.pauseAIService()
	}
	if payload.ExternalFileAccessPaused && !previous.ExternalFileAccessPaused {
		s.cancelExternalMediaWork()
	}
	if wasEffectivelyPaused && !isEffectivelyPaused {
		s.scanner.RequestMediaScan("debug_resume")
	}
	writeJSON(w, http.StatusOK, debugSettingsResponse(payload))
}

func debugSettingsResponse(settings db.DebugSettings) debugSettingsDTO {
	return debugSettingsDTO{
		ExternalFileAccessPaused:   settings.ExternalFileAccessPaused,
		BackgroundProcessingPaused: settings.BackgroundProcessingPaused,
		EffectiveBackgroundPaused:  settings.ExternalFileAccessPaused || settings.BackgroundProcessingPaused,
	}
}

func (s *Server) cancelExternalMediaWork() {
	// Running HTTP source copies stop with their request context. Explicitly
	// cancel server-owned FFmpeg and read-ahead work immediately.
	s.videoFullWarmMu.Lock()
	for key, job := range s.videoFullWarmJobs {
		if job != nil && job.cancel != nil {
			job.cancel()
		}
		delete(s.videoFullWarmJobs, key)
	}
	s.videoFullWarmMu.Unlock()

	s.videoProxyMu.Lock()
	for _, state := range s.videoProxyStates {
		if state != nil && state.Cancel != nil {
			state.Cancel()
		}
	}
	for _, state := range s.videoSegmentStates {
		if state != nil && state.Cancel != nil {
			state.Cancel(debugcontrol.ErrExternalFileAccessPaused)
		}
	}
	s.videoProxyMu.Unlock()

	s.audioProxyMu.Lock()
	for _, state := range s.audioProxyStates {
		if state == nil {
			continue
		}
		state.mu.Lock()
		if state.cancel != nil {
			state.cancel()
		}
		state.mu.Unlock()
	}
	s.audioProxyMu.Unlock()

}

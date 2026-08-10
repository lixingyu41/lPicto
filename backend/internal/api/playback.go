package api

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) markAssetPlayed(w http.ResponseWriter, r *http.Request) {
	id, ok := s.idParam(w, r)
	if !ok {
		return
	}
	playedAt, err := s.db.MarkAssetPlayed(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "video_not_found", "视频不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "playback_record_failed", "记录播放时间失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recorded": true, "lastPlayedAt": playedAt})
}

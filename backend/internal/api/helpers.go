package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, ErrorResponse{Error: APIError{Code: code, Message: message}})
}

func intQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64QueryPtr(r *http.Request, key string) *int64 {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func int64ListQuery(r *http.Request, key string) []int64 {
	values := r.URL.Query()[key]
	if len(values) == 0 {
		return nil
	}
	seen := map[int64]struct{}{}
	var result []int64
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			parsed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil || parsed <= 0 {
				continue
			}
			if _, ok := seen[parsed]; ok {
				continue
			}
			seen[parsed] = struct{}{}
			result = append(result, parsed)
		}
	}
	return result
}

func intQueryPtr(r *http.Request, key string) *int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func ratingQueryPtr(r *http.Request, key string) *int {
	value := intQueryPtr(r, key)
	if value == nil || *value < 0 || *value > 5 {
		return nil
	}
	return value
}

func albumUnassignedQuery(r *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("albumFilter")))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("album")))
	}
	return value == "none" || value == "unassigned"
}

func float64QueryPtr(r *http.Request, key string) *float64 {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func ClampPage(page int, pageSize int, defaultPageSize int, maxPageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func safeSort(value string) string {
	if value == "filename" || value == "size" {
		return value
	}
	parts := strings.Split(value, "_")
	if len(parts) < 2 {
		return "timeline_desc"
	}
	direction := parts[len(parts)-1]
	field := strings.Join(parts[:len(parts)-1], "_")
	if direction != "asc" && direction != "desc" {
		return "timeline_desc"
	}
	switch field {
	case "timeline", "imported", "filename", "path", "media_type", "resolution", "duration", "modified", "size", "rating",
		"container", "video_codec", "audio_codec", "fps", "bitrate", "subtitle", "danmaku", "ai_description", "ai_tag":
		return value
	}
	return "timeline_desc"
}

func safeGroup(value string) string {
	switch value {
	case "day", "month", "year", "size", "letter", "folder":
		return value
	default:
		return ""
	}
}

func safeType(value string) string {
	switch value {
	case "image", "video", "audio":
		return value
	default:
		return "all"
	}
}

func safeOrientation(value string) string {
	switch value {
	case "landscape", "portrait":
		return value
	default:
		return "all"
	}
}

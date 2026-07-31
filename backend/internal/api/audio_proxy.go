package api

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"lpicto/backend/internal/model"
)

type audioProxyRuntime struct {
	mu        sync.Mutex
	AssetID   int64
	Status    string
	Priority  string
	Progress  float64
	Bytes     int64
	Error     string
	UpdatedAt int64
	cancel    context.CancelFunc
}

type audioProxyStatusDTO struct {
	AssetID     int64   `json:"assetId"`
	Required    bool    `json:"required"`
	Cached      bool    `json:"cached"`
	Transcoding bool   `json:"transcoding"`
	Queued      bool    `json:"queued"`
	Status      string  `json:"status"`
	Progress    float64 `json:"progress"`
	Bytes       int64   `json:"bytes"`
	Error       string  `json:"error"`
	UpdatedAt   int64   `json:"updatedAt"`
}

func (s *Server) serveAudioAsset(w http.ResponseWriter, r *http.Request, asset model.Asset) {
	stopPriority := s.holdPlaybackPriority(r.Context())
	defer stopPriority()
	if asset.BrowserPlayable {
		if s.serveChunkCachedAudio(w, r, asset) {
			return
		}
		writeError(w, http.StatusServiceUnavailable, "audio_source_unavailable", "音频源文件暂时不可用")
		return
	}
	path, err := s.store.CachePath("audio-proxies", asset.CacheKey, "flac")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audio_proxy_path_failed", "读取音频兼容缓存失败")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusConflict, "audio_proxy_not_ready", "音频兼容缓存尚未生成")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusConflict, "audio_proxy_not_ready", "音频兼容缓存尚未生成")
		return
	}
	s.cachePolicy.Touch(r.Context(), "audio-proxies", asset.CacheKey, path)
	w.Header().Set("Content-Type", "audio/flac")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", fmt.Sprintf(`W/"audio-%d-%s"`, asset.ID, asset.CacheKey))
	w.Header().Set("Content-Disposition", contentDisposition(asset.Filename+".flac"))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Accel-Buffering", "no")
	http.ServeContent(w, r, asset.Filename+".flac", info.ModTime(), file)
}

func (s *Server) startAudioProxy(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeAudio {
		writeError(w, http.StatusBadRequest, "not_audio", "资源不是音频")
		return
	}
	priority := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("priority")))
	if priority != "preload" {
		priority = "current"
	}
	if asset.BrowserPlayable {
		writeJSON(w, http.StatusOK, s.audioProxySnapshot(asset, nil))
		return
	}
	path, _ := s.store.CachePath("audio-proxies", asset.CacheKey, "flac")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		writeJSON(w, http.StatusOK, s.audioProxySnapshot(asset, nil))
		return
	}
	s.audioProxyMu.Lock()
	if priority == "current" {
		for key, candidate := range s.audioProxyStates {
			if key == asset.CacheKey || candidate == nil {
				continue
			}
			candidate.mu.Lock()
			if candidate.Priority == "preload" && (candidate.Status == "queued" || candidate.Status == "processing") && candidate.cancel != nil {
				candidate.cancel()
			}
			candidate.mu.Unlock()
		}
	}
	state := s.audioProxyStates[asset.CacheKey]
	restart := state == nil
	if state != nil {
		state.mu.Lock()
		restart = state.Status == "idle" || state.Status == "error"
		state.mu.Unlock()
	}
	if restart {
		state = &audioProxyRuntime{AssetID: asset.ID, Status: "queued", Priority: priority, UpdatedAt: time.Now().Unix()}
		s.audioProxyStates[asset.CacheKey] = state
		ctx, cancel := context.WithCancel(context.Background())
		state.cancel = cancel
		go s.runAudioProxy(ctx, asset, state)
	} else if priority == "current" {
		state.mu.Lock()
		state.Priority = "current"
		state.UpdatedAt = time.Now().Unix()
		state.mu.Unlock()
	}
	s.audioProxyMu.Unlock()
	writeJSON(w, http.StatusAccepted, s.audioProxySnapshot(asset, state))
}

func (s *Server) audioProxyStatus(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeAudio {
		writeError(w, http.StatusBadRequest, "not_audio", "资源不是音频")
		return
	}
	s.audioProxyMu.Lock()
	state := s.audioProxyStates[asset.CacheKey]
	s.audioProxyMu.Unlock()
	writeJSON(w, http.StatusOK, s.audioProxySnapshot(asset, state))
}

func (s *Server) audioProxySnapshot(asset model.Asset, state *audioProxyRuntime) audioProxyStatusDTO {
	result := audioProxyStatusDTO{AssetID: asset.ID, Required: !asset.BrowserPlayable, Status: "idle"}
	path, _ := s.store.CachePath("audio-proxies", asset.CacheKey, "flac")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		result.Cached, result.Status, result.Progress, result.Bytes = true, "ready", 1, info.Size()
		return result
	}
	if state == nil {
		return result
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	result.Status = state.Status
	result.Progress = state.Progress
	result.Bytes = state.Bytes
	result.Error = state.Error
	result.UpdatedAt = state.UpdatedAt
	result.Queued = state.Status == "queued"
	result.Transcoding = state.Status == "processing"
	return result
}

func (s *Server) runAudioProxy(ctx context.Context, asset model.Asset, state *audioProxyRuntime) {
	select {
	case s.audioProxySlot <- struct{}{}:
		defer func() { <-s.audioProxySlot }()
	case <-ctx.Done():
		s.finishAudioProxy(state, "idle", nil)
		return
	}
	state.mu.Lock()
	state.Status, state.UpdatedAt = "processing", time.Now().Unix()
	priority := state.Priority
	state.mu.Unlock()
	var stopPriority func()
	if priority == "current" {
		stopPriority = s.holdPlaybackPriority(ctx)
		defer stopPriority()
	}
	source, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		s.finishAudioProxy(state, "error", err)
		return
	}
	if available, _ := s.sourceHealth.AvailableForRel(asset.RelPath); !available {
		s.finishAudioProxy(state, "error", fmt.Errorf("源文件暂时不可用"))
		return
	}
	dest, err := s.store.CachePath("audio-proxies", asset.CacheKey, "flac")
	if err != nil {
		s.finishAudioProxy(state, "error", err)
		return
	}
	if _, err := s.cachePolicy.EnsureCapacity(ctx, max(asset.Size, 64<<20)); err != nil {
		s.finishAudioProxy(state, "error", err)
		return
	}
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-nostats", "-progress", "pipe:1", "-i", source, "-map", "0:a:0", "-vn", "-c:a", "flac", "-compression_level", "5", "-f", "flac", tmp}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.finishAudioProxy(state, "error", err)
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	batchID, _ := s.db.BeginSourceIOBatch(context.Background(), "audio_proxy", 0)
	if err := cmd.Start(); err != nil {
		_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "failed", 1, 0, err.Error())
		s.finishAudioProxy(state, "error", err)
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.trackAudioProxyProgress(stdout, asset.Duration, state)
	}()
	err = cmd.Wait()
	<-done
	if ctx.Err() != nil {
		_ = os.Remove(tmp)
		_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "interrupted", 1, 0, "被当前媒体播放打断")
		s.finishAudioProxy(state, "idle", nil)
		return
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		_ = os.Remove(tmp)
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "failed", 1, 0, message)
		s.finishAudioProxy(state, "error", fmt.Errorf("%s", message))
		return
	}
	info, _ := os.Stat(dest)
	assetID := asset.ID
	s.cachePolicy.Register(context.Background(), "audio-proxies", asset.CacheKey, dest, &assetID, 10*time.Minute)
	bytes := int64(0)
	if info != nil {
		bytes = info.Size()
	}
	_ = s.db.FinishSourceIOBatch(context.Background(), batchID, "success", 1, asset.Size, "")
	state.mu.Lock()
	state.Status, state.Progress, state.Bytes, state.Error, state.UpdatedAt = "ready", 1, bytes, "", time.Now().Unix()
	state.mu.Unlock()
}

func (s *Server) trackAudioProxyProgress(reader io.Reader, duration *float64, state *audioProxyRuntime) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "out_time_ms=") || duration == nil || *duration <= 0 {
			continue
		}
		microseconds, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_ms="), 10, 64)
		if err != nil {
			continue
		}
		progress := float64(microseconds) / 1_000_000 / *duration
		if progress > 0.99 {
			progress = 0.99
		}
		state.mu.Lock()
		state.Progress, state.UpdatedAt = progress, time.Now().Unix()
		state.mu.Unlock()
	}
}

func (s *Server) finishAudioProxy(state *audioProxyRuntime, status string, err error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Status, state.UpdatedAt = status, time.Now().Unix()
	if err != nil {
		state.Error = err.Error()
	} else {
		state.Error = ""
	}
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

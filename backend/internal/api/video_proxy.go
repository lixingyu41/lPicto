package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lpicto/backend/internal/model"
	videoproc "lpicto/backend/internal/video"
)

const (
	videoProxyCacheTTL     = 20 * time.Minute
	videoProxyKeepaliveTTL = 45 * time.Second
	videoProxySweepEvery   = 1 * time.Minute
	videoProxyReadDelay    = 250 * time.Millisecond
	videoProxyIdleCheck    = 2 * time.Second
	videoProxyOpenTimeout  = 90 * time.Second
)

var errVideoProxyIdle = errors.New("video proxy idle")

type videoProxyRuntime struct {
	AssetID      int64
	CacheKey     string
	StartSeconds float64
	DestPath     string
	TempPath     string
	Duration     float64
	Queued       bool
	Transcoding  bool
	StartedAt    time.Time
	UpdatedAt    time.Time
	ExpiresAt    time.Time
	LeaseUntil   time.Time
	Progress     float64
	SecondsDone  float64
	Bytes        int64
	Error        string
	ActiveStream int
	Sessions     map[string]*videoProxySession
	Done         chan struct{}
}

type videoProxySession struct {
	ClientID     string
	SessionID    string
	State        string
	CurrentTime  float64
	PlaybackRate float64
	WantsStream  bool
	Hidden       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LeaseUntil   time.Time
}

func (s *Server) videoProxy(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	if asset.BrowserPlayable {
		s.serveOriginalAsset(w, r, asset)
		return
	}
	if !s.cfg.VideoProxyEnabled {
		writeError(w, http.StatusNotFound, "video_proxy_disabled", "视频代理未启用")
		return
	}
	if !videoProxyPlaybackRequested(r) {
		writeError(w, http.StatusConflict, "video_proxy_not_started", "视频未开始播放")
		return
	}
	if missing, err := s.assetSourceMissing(asset); err != nil {
		s.logger.Warn("check video proxy source failed", "assetID", asset.ID, "relPath", asset.RelPath, "error", err)
	} else if missing {
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "源文件暂时不可用")
		return
	}
	startSeconds := videoProxyStartSeconds(r, asset)
	session := videoProxySessionFromRequest(r)
	state, cached, err := s.ensureVideoProxyRuntime(asset, startSeconds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "video_proxy_failed", "启动视频转码失败")
		return
	}
	if cached {
		s.serveCachedVideoProxy(w, r, asset, state, session.SessionID)
		return
	}
	s.serveLiveVideoProxy(w, r, asset, state, session.SessionID)
}

func (s *Server) videoProxyStatus(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	session := videoProxySessionFromRequest(r)
	dto, err := s.videoProxyRuntimeDTO(asset, videoProxyStartSeconds(r, asset), session.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "video_proxy_status_failed", "读取转码状态失败")
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) videoProxyKeepalive(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	startSeconds := videoProxyStartSeconds(r, asset)
	heartbeat := videoProxyHeartbeatFromRequest(r)
	dto, err := s.touchVideoProxyRuntime(asset, true, startSeconds, heartbeat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "video_proxy_keepalive_failed", "刷新转码缓存失败")
		return
	}
	if videoProxyHeartbeatWantsRuntime(heartbeat) {
		if _, _, err := s.ensureVideoProxyRuntime(asset, startSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, "video_proxy_failed", "启动视频转码失败")
			return
		}
		dto, err = s.videoProxyRuntimeDTO(asset, startSeconds, heartbeat.SessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "video_proxy_status_failed", "读取转码状态失败")
			return
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) ensureVideoProxyRuntime(asset model.Asset, startSeconds float64) (*videoProxyRuntime, bool, error) {
	runtimeKey := videoProxyRuntimeKey(asset.CacheKey, startSeconds)
	dest, err := s.store.CachePath("video-proxies", runtimeKey, "mp4")
	if err != nil {
		return nil, false, err
	}
	tmp := dest + ".tmp.mp4"
	now := time.Now()
	s.videoProxyMu.Lock()
	state := s.videoProxyStates[runtimeKey]
	if state == nil {
		state = &videoProxyRuntime{
			AssetID:      asset.ID,
			CacheKey:     runtimeKey,
			StartSeconds: startSeconds,
			DestPath:     dest,
			TempPath:     tmp,
			Duration:     assetDuration(asset),
			UpdatedAt:    now,
			ExpiresAt:    now.Add(videoProxyCacheTTL),
			Sessions:     map[string]*videoProxySession{},
			Done:         make(chan struct{}),
		}
		s.videoProxyStates[runtimeKey] = state
	}
	if state.Sessions == nil {
		state.Sessions = map[string]*videoProxySession{}
	}
	state.AssetID = asset.ID
	state.CacheKey = runtimeKey
	state.StartSeconds = startSeconds
	state.DestPath = dest
	state.TempPath = tmp
	state.Duration = assetDuration(asset)
	state.UpdatedAt = now
	state.ExpiresAt = now.Add(videoProxyCacheTTL)
	if fileInfo, err := os.Stat(dest); err == nil && !fileInfo.IsDir() {
		state.Transcoding = false
		state.Progress = 1
		state.SecondsDone = state.Duration
		state.Bytes = fileInfo.Size()
		state.Error = ""
		s.videoProxyMu.Unlock()
		_ = touchFile(dest, now)
		return state, true, nil
	}
	if state.Queued || state.Transcoding {
		s.videoProxyMu.Unlock()
		return state, false, nil
	}
	state.Queued = true
	state.Transcoding = false
	state.StartedAt = now
	state.Progress = 0
	state.SecondsDone = 0
	state.Bytes = 0
	state.Error = ""
	state.Done = make(chan struct{})
	s.videoProxyMu.Unlock()
	go s.runVideoProxyTranscode(asset, runtimeKey, startSeconds, dest, tmp)
	return state, false, nil
}

func (s *Server) serveCachedVideoProxy(w http.ResponseWriter, r *http.Request, asset model.Asset, state *videoProxyRuntime, sessionID string) {
	file, err := os.Open(state.DestPath)
	if err != nil {
		s.markVideoProxyError(state.CacheKey, err)
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	s.markVideoProxyStream(state.CacheKey, sessionID, true)
	defer s.markVideoProxyStream(state.CacheKey, sessionID, false)
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "public, max-age=1200")
	w.Header().Set("ETag", `"`+state.CacheKey+`"`)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Accel-Buffering", "no")
	http.ServeContent(w, r, filepath.Base(state.DestPath), info.ModTime(), file)
}

func (s *Server) serveLiveVideoProxy(w http.ResponseWriter, r *http.Request, asset model.Asset, state *videoProxyRuntime, sessionID string) {
	s.markVideoProxyStream(state.CacheKey, sessionID, true)
	defer s.markVideoProxyStream(state.CacheKey, sessionID, false)
	reader, err := openGrowingFile(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "video_proxy_failed", "视频转码失败")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Accept-Ranges", "none")
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, 256*1024)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr == nil {
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			return
		}
		if !s.videoProxyStillRunning(state.CacheKey) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(videoProxyReadDelay):
		}
	}
}

func openGrowingFile(ctx context.Context, state *videoProxyRuntime) (*os.File, error) {
	deadline := time.Now().Add(videoProxyOpenTimeout)
	for {
		if file, err := os.Open(state.TempPath); err == nil {
			return file, nil
		}
		if file, err := os.Open(state.DestPath); err == nil {
			return file, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("video proxy output not available")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Server) runVideoProxyTranscode(asset model.Asset, runtimeKey string, startSeconds float64, dest string, tmp string) {
	_ = os.Remove(tmp)
	source, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	if err := s.db.SetAssetWorkStatus(context.Background(), asset.ID, "video_proxy_status", model.StatusProcessing, nil); err != nil && s.logger != nil {
		s.logger.Warn("set video proxy processing failed", "assetID", asset.ID, "error", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	idleErr := make(chan error, 1)
	go s.cancelVideoProxyTranscodeWhenIdle(ctx, cancel, runtimeKey, idleErr)
	releaseSlot, err := s.acquireVideoProxySlot(ctx)
	if err != nil {
		if isVideoProxyIdleStop(idleErr) || errors.Is(err, context.Canceled) {
			err = errVideoProxyIdle
		}
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	defer releaseSlot()
	s.markVideoProxyTranscodeStarted(runtimeKey)
	args := videoproc.StreamProxyArgs(source, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, s.cfg.FFmpegHWDevice, startSeconds)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	output, err := os.Create(tmp)
	if err != nil {
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
		return
	}
	errText := make(chan string, 1)
	go func() {
		errText <- s.readVideoProxyProgress(runtimeKey, assetDuration(asset), startSeconds, stderr)
	}()
	_, copyErr := io.Copy(output, stdout)
	closeErr := output.Close()
	waitErr := cmd.Wait()
	progressText := <-errText
	if isVideoProxyIdleStop(idleErr) {
		err = errVideoProxyIdle
	} else if copyErr != nil {
		err = copyErr
	} else if closeErr != nil {
		err = closeErr
	} else if waitErr != nil {
		if strings.TrimSpace(progressText) != "" {
			err = fmt.Errorf("%w: %s", waitErr, strings.TrimSpace(progressText))
		} else {
			err = waitErr
		}
	}
	s.finishVideoProxyTranscode(asset, runtimeKey, dest, tmp, err)
}

func (s *Server) cancelVideoProxyTranscodeWhenIdle(ctx context.Context, cancel context.CancelFunc, cacheKey string, idleErr chan<- error) {
	ticker := time.NewTicker(videoProxyIdleCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.videoProxyTranscodeIdle(cacheKey, time.Now()) {
				continue
			}
			select {
			case idleErr <- errVideoProxyIdle:
			default:
			}
			cancel()
			return
		}
	}
}

func (s *Server) videoProxyTranscodeIdle(cacheKey string, now time.Time) bool {
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[cacheKey]
	if state == nil || (!state.Queued && !state.Transcoding) {
		return false
	}
	state.LeaseUntil = videoProxyMaxSessionLease(state, now)
	return !state.LeaseUntil.After(now)
}

func (s *Server) acquireVideoProxySlot(ctx context.Context) (func(), error) {
	if s.videoProxySlots == nil {
		return func() {}, nil
	}
	select {
	case s.videoProxySlots <- struct{}{}:
		released := false
		return func() {
			if released {
				return
			}
			released = true
			<-s.videoProxySlots
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Server) markVideoProxyTranscodeStarted(cacheKey string) {
	now := time.Now()
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[cacheKey]
	if state == nil {
		return
	}
	state.Queued = false
	state.Transcoding = true
	state.StartedAt = now
	state.UpdatedAt = now
}

func isVideoProxyIdleStop(idleErr <-chan error) bool {
	select {
	case err := <-idleErr:
		return errors.Is(err, errVideoProxyIdle)
	default:
		return false
	}
}

func videoProxyPlaybackRequested(r *http.Request) bool {
	value := r.URL.Query().Get("play")
	return value == "1" || strings.EqualFold(value, "true")
}

func videoProxyStartSeconds(r *http.Request, asset model.Asset) float64 {
	raw := strings.TrimSpace(r.URL.Query().Get("start"))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	duration := assetDuration(asset)
	if duration > 0 && value >= duration {
		value = math.Max(0, duration-1)
	}
	return math.Round(value*100) / 100
}

func videoProxySessionFromRequest(r *http.Request) VideoProxyHeartbeatRequest {
	query := r.URL.Query()
	return sanitizeVideoProxyHeartbeat(VideoProxyHeartbeatRequest{
		ClientID:  query.Get("clientId"),
		SessionID: query.Get("sessionId"),
		State:     query.Get("state"),
	})
}

func videoProxyHeartbeatFromRequest(r *http.Request) VideoProxyHeartbeatRequest {
	heartbeat := videoProxySessionFromRequest(r)
	if r.Body != nil {
		var body VideoProxyHeartbeatRequest
		err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		if err == nil {
			if body.ClientID != "" {
				heartbeat.ClientID = body.ClientID
			}
			if body.SessionID != "" {
				heartbeat.SessionID = body.SessionID
			}
			if body.State != "" {
				heartbeat.State = body.State
			}
			heartbeat.CurrentTime = body.CurrentTime
			heartbeat.PlaybackRate = body.PlaybackRate
			heartbeat.WantsStream = body.WantsStream
			heartbeat.Hidden = body.Hidden
		}
	}
	heartbeat = sanitizeVideoProxyHeartbeat(heartbeat)
	if heartbeat.State == "" {
		heartbeat.State = "playing"
	}
	return heartbeat
}

func sanitizeVideoProxyHeartbeat(heartbeat VideoProxyHeartbeatRequest) VideoProxyHeartbeatRequest {
	heartbeat.ClientID = sanitizeVideoProxyID(heartbeat.ClientID, "browser")
	heartbeat.SessionID = sanitizeVideoProxyID(heartbeat.SessionID, "legacy")
	heartbeat.State = normalizeVideoProxySessionState(heartbeat.State)
	if !isFinitePositiveOrZero(heartbeat.CurrentTime) {
		heartbeat.CurrentTime = 0
	}
	if !isFinitePositiveOrZero(heartbeat.PlaybackRate) || heartbeat.PlaybackRate == 0 {
		heartbeat.PlaybackRate = 1
	}
	return heartbeat
}

func sanitizeVideoProxyID(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if len(value) > 96 {
		value = value[:96]
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return fallback
	}
	return builder.String()
}

func normalizeVideoProxySessionState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "preparing", "playing", "paused", "stopped":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func isFinitePositiveOrZero(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func videoProxyHeartbeatWantsRuntime(heartbeat VideoProxyHeartbeatRequest) bool {
	return heartbeat.WantsStream || heartbeat.State == "preparing" || heartbeat.State == "playing"
}

func videoProxyRuntimeKey(cacheKey string, startSeconds float64) string {
	if startSeconds <= 0 {
		return cacheKey
	}
	return fmt.Sprintf("%s-s%d", cacheKey, int64(math.Round(startSeconds*100)))
}

func (s *Server) readVideoProxyProgress(cacheKey string, duration float64, startSeconds float64, stderr io.Reader) string {
	var errorLines []string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "out_time_ms=") {
			ms, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_ms="), 64)
			seconds := ms / 1000000
			s.updateVideoProxyProgress(cacheKey, startSeconds+seconds, duration)
			continue
		}
		if strings.HasPrefix(line, "out_time_us=") {
			us, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
			seconds := us / 1000000
			s.updateVideoProxyProgress(cacheKey, startSeconds+seconds, duration)
			continue
		}
		if strings.HasPrefix(line, "progress=") || strings.Contains(line, "=") {
			continue
		}
		errorLines = append(errorLines, line)
		if len(errorLines) > 6 {
			errorLines = errorLines[1:]
		}
	}
	return strings.Join(errorLines, "\n")
}

func (s *Server) updateVideoProxyProgress(cacheKey string, seconds float64, duration float64) {
	now := time.Now()
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[cacheKey]
	if state == nil {
		return
	}
	if duration <= 0 {
		duration = state.Duration
	}
	if seconds < 0 {
		seconds = 0
	}
	state.SecondsDone = seconds
	state.Duration = duration
	if duration > 0 {
		state.Progress = minFloat(1, maxFloat(0, seconds/duration))
	}
	state.UpdatedAt = now
}

func (s *Server) finishVideoProxyTranscode(asset model.Asset, runtimeKey string, dest string, tmp string, err error) {
	now := time.Now()
	var message *string
	status := model.StatusReady
	idleStop := errors.Is(err, errVideoProxyIdle)
	if idleStop {
		status = model.StatusPending
		_ = os.Remove(tmp)
	} else if err != nil {
		text := videoProxyPublicError(err)
		message = &text
		status = model.StatusError
		_ = os.Remove(tmp)
	} else if renameErr := os.Rename(tmp, dest); renameErr != nil {
		text := videoProxyPublicError(renameErr)
		message = &text
		status = model.StatusError
		err = renameErr
		_ = os.Remove(tmp)
	} else {
		_ = touchFile(dest, now)
	}
	if dbErr := s.db.SetAssetWorkStatus(context.Background(), asset.ID, "video_proxy_status", status, message); dbErr != nil && s.logger != nil {
		s.logger.Warn("set video proxy final status failed", "assetID", asset.ID, "status", status, "error", dbErr)
	}
	s.videoProxyMu.Lock()
	state := s.videoProxyStates[runtimeKey]
	if state != nil {
		state.Queued = false
		state.Transcoding = false
		state.UpdatedAt = now
		state.ExpiresAt = now.Add(videoProxyCacheTTL)
		if idleStop {
			state.Progress = 0
			state.SecondsDone = 0
			state.Bytes = 0
			state.Error = ""
			state.ExpiresAt = now
		} else if err != nil {
			state.Error = videoProxyPublicError(err)
		} else {
			state.Progress = 1
			state.SecondsDone = state.Duration
			state.Error = ""
			if info, statErr := os.Stat(dest); statErr == nil {
				state.Bytes = info.Size()
			}
		}
		close(state.Done)
	}
	s.videoProxyMu.Unlock()
	if err != nil && !idleStop && s.logger != nil {
		s.logger.Warn("video proxy transcode failed", "assetID", asset.ID, "relPath", asset.RelPath, "error", err)
	}
}

func (s *Server) markVideoProxyStream(cacheKey string, sessionID string, active bool) {
	now := time.Now()
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[cacheKey]
	if state == nil {
		return
	}
	if active {
		state.ActiveStream++
		state.LeaseUntil = now.Add(videoProxyKeepaliveTTL)
		session := videoProxyRuntimeSession(state, "", sessionID, now)
		session.State = "playing"
		session.WantsStream = true
		session.UpdatedAt = now
		session.LeaseUntil = state.LeaseUntil
	} else if state.ActiveStream > 0 {
		state.ActiveStream--
	}
	state.UpdatedAt = now
	state.ExpiresAt = now.Add(videoProxyCacheTTL)
}

func (s *Server) videoProxyStillRunning(cacheKey string) bool {
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[cacheKey]
	return state != nil && state.Transcoding
}

func (s *Server) markVideoProxyError(cacheKey string, err error) {
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	if state := s.videoProxyStates[cacheKey]; state != nil {
		state.Error = videoProxyPublicError(err)
		state.UpdatedAt = time.Now()
	}
}

func (s *Server) videoProxyRuntimeDTO(asset model.Asset, startSeconds float64, sessionID string) (VideoProxyRuntimeDTO, error) {
	return s.touchVideoProxyRuntime(asset, false, startSeconds, VideoProxyHeartbeatRequest{SessionID: sessionID})
}

func (s *Server) touchVideoProxyRuntime(asset model.Asset, keepalive bool, startSeconds float64, heartbeat VideoProxyHeartbeatRequest) (VideoProxyRuntimeDTO, error) {
	now := time.Now()
	required := asset.MediaType == model.MediaTypeVideo && !asset.BrowserPlayable && s.cfg.VideoProxyEnabled
	dest := ""
	tmp := ""
	runtimeKey := videoProxyRuntimeKey(asset.CacheKey, startSeconds)
	heartbeat = sanitizeVideoProxyHeartbeat(heartbeat)
	if required {
		var err error
		dest, err = s.store.CachePath("video-proxies", runtimeKey, "mp4")
		if err != nil {
			return VideoProxyRuntimeDTO{}, err
		}
		tmp = dest + ".tmp.mp4"
	}
	s.videoProxyMu.Lock()
	state := s.videoProxyStates[runtimeKey]
	if required && state == nil {
		state = &videoProxyRuntime{
			AssetID:      asset.ID,
			CacheKey:     runtimeKey,
			StartSeconds: startSeconds,
			DestPath:     dest,
			TempPath:     tmp,
			Duration:     assetDuration(asset),
			UpdatedAt:    now,
			ExpiresAt:    now.Add(videoProxyCacheTTL),
			Sessions:     map[string]*videoProxySession{},
			Done:         make(chan struct{}),
		}
		s.videoProxyStates[runtimeKey] = state
	}
	if state != nil {
		if state.Sessions == nil {
			state.Sessions = map[string]*videoProxySession{}
		}
		state.AssetID = asset.ID
		state.CacheKey = runtimeKey
		state.StartSeconds = startSeconds
		state.Duration = assetDuration(asset)
		state.UpdatedAt = now
		if keepalive {
			s.applyVideoProxyHeartbeatLocked(state, heartbeat, now)
		}
		state.LeaseUntil = videoProxyMaxSessionLease(state, now)
	}
	s.videoProxyMu.Unlock()
	if keepalive && dest != "" {
		_ = touchFile(dest, now)
	}
	return s.snapshotVideoProxyRuntime(asset, required, runtimeKey, dest, heartbeat.SessionID), nil
}

func (s *Server) snapshotVideoProxyRuntime(asset model.Asset, required bool, runtimeKey string, dest string, sessionID string) VideoProxyRuntimeDTO {
	now := time.Now()
	dto := VideoProxyRuntimeDTO{
		Required:     required,
		Status:       "not_required",
		Duration:     assetDuration(asset),
		UpdatedAt:    now.Unix(),
		CacheTTL:     int64(videoProxyCacheTTL.Seconds()),
		KeepaliveTTL: int64(videoProxyKeepaliveTTL.Seconds()),
		RuntimeKey:   runtimeKey,
		SessionID:    sessionID,
		Command:      "none",
		Message:      "",
		ServerTime:   now.Unix(),
	}
	if !required {
		return dto
	}
	if info, err := os.Stat(dest); err == nil && !info.IsDir() {
		dto.Cached = true
		dto.Status = "cached"
		dto.Progress = 1
		dto.SecondsDone = dto.Duration
		dto.Bytes = info.Size()
		dto.ExpiresAt = info.ModTime().Add(videoProxyCacheTTL).Unix()
	}
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoProxyStates[runtimeKey]
	if state == nil {
		if dto.Cached {
			return dto
		}
		dto.Status = "idle"
		return dto
	}
	dto.Transcoding = state.Transcoding
	dto.Queued = state.Queued
	activeUsers, playingUsers, currentSession := videoProxySessionStats(state, sessionID, now)
	state.LeaseUntil = videoProxyMaxSessionLease(state, now)
	dto.Active = state.ActiveStream > 0 || state.LeaseUntil.After(now)
	dto.Progress = state.Progress
	dto.SecondsDone = state.SecondsDone
	dto.Duration = state.Duration
	dto.Bytes = maxInt64(dto.Bytes, state.Bytes)
	dto.Error = state.Error
	dto.UpdatedAt = state.UpdatedAt.Unix()
	dto.LeaseUntil = unixTime(state.LeaseUntil)
	dto.ActiveUsers = activeUsers
	dto.PlayingUsers = playingUsers
	if currentSession != nil {
		dto.ClientID = currentSession.ClientID
		dto.SessionState = currentSession.State
	}
	if state.ExpiresAt.After(now) {
		dto.ExpiresAt = state.ExpiresAt.Unix()
	}
	if state.Queued {
		dto.Status = "queued"
	} else if state.Transcoding {
		dto.Status = "transcoding"
	} else if state.Error != "" {
		dto.Status = "error"
	} else if dto.Cached {
		dto.Status = "cached"
	} else {
		dto.Status = "idle"
	}
	dto.Command, dto.Message = videoProxyRuntimeInstruction(dto)
	return dto
}

func (s *Server) applyVideoProxyHeartbeatLocked(state *videoProxyRuntime, heartbeat VideoProxyHeartbeatRequest, now time.Time) {
	if state.Sessions == nil {
		state.Sessions = map[string]*videoProxySession{}
	}
	if heartbeat.State == "stopped" {
		delete(state.Sessions, heartbeat.SessionID)
		state.LeaseUntil = videoProxyMaxSessionLease(state, now)
		state.UpdatedAt = now
		return
	}
	session := videoProxyRuntimeSession(state, heartbeat.ClientID, heartbeat.SessionID, now)
	session.State = heartbeat.State
	session.CurrentTime = heartbeat.CurrentTime
	session.PlaybackRate = heartbeat.PlaybackRate
	session.WantsStream = heartbeat.WantsStream
	session.Hidden = heartbeat.Hidden
	session.UpdatedAt = now
	if videoProxyHeartbeatWantsRuntime(heartbeat) {
		session.LeaseUntil = now.Add(videoProxyKeepaliveTTL)
		state.ExpiresAt = now.Add(videoProxyCacheTTL)
	}
	state.LeaseUntil = videoProxyMaxSessionLease(state, now)
	state.UpdatedAt = now
}

func videoProxyRuntimeSession(state *videoProxyRuntime, clientID string, sessionID string, now time.Time) *videoProxySession {
	if state.Sessions == nil {
		state.Sessions = map[string]*videoProxySession{}
	}
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	clientID = sanitizeVideoProxyID(clientID, "browser")
	session := state.Sessions[sessionID]
	if session == nil {
		session = &videoProxySession{
			ClientID:     clientID,
			SessionID:    sessionID,
			State:        "preparing",
			PlaybackRate: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		state.Sessions[sessionID] = session
	}
	if clientID != "" {
		session.ClientID = clientID
	}
	return session
}

func videoProxyMaxSessionLease(state *videoProxyRuntime, now time.Time) time.Time {
	if state == nil || len(state.Sessions) == 0 {
		return time.Time{}
	}
	var lease time.Time
	for id, session := range state.Sessions {
		if session == nil {
			delete(state.Sessions, id)
			continue
		}
		if session.LeaseUntil.IsZero() && now.Sub(session.UpdatedAt) > videoProxyKeepaliveTTL {
			delete(state.Sessions, id)
			continue
		}
		if !session.LeaseUntil.IsZero() && !session.LeaseUntil.After(now) && now.Sub(session.UpdatedAt) > videoProxyKeepaliveTTL {
			delete(state.Sessions, id)
			continue
		}
		if session.LeaseUntil.After(lease) {
			lease = session.LeaseUntil
		}
	}
	return lease
}

func videoProxySessionStats(state *videoProxyRuntime, sessionID string, now time.Time) (int, int, *videoProxySession) {
	if state == nil {
		return 0, 0, nil
	}
	if state.Sessions == nil {
		state.Sessions = map[string]*videoProxySession{}
	}
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	var current *videoProxySession
	active := 0
	playing := 0
	for id, session := range state.Sessions {
		if session == nil {
			delete(state.Sessions, id)
			continue
		}
		if session.SessionID == sessionID {
			current = session
		}
		if session.LeaseUntil.After(now) {
			active++
			if session.State == "playing" || session.State == "preparing" || session.WantsStream {
				playing++
			}
		}
	}
	return active, playing, current
}

func videoProxyRuntimeInstruction(dto VideoProxyRuntimeDTO) (string, string) {
	if !dto.Required {
		return "none", ""
	}
	if dto.Status == "error" {
		if strings.TrimSpace(dto.Error) != "" {
			return "show_error", dto.Error
		}
		return "show_error", "转码失败"
	}
	if dto.Status == "cached" || dto.Cached {
		return "use_cache", "转码缓存可用"
	}
	if dto.Status == "queued" || dto.Queued {
		return "wait_slot", "等待转码槽位"
	}
	if dto.Status == "transcoding" || dto.Transcoding {
		return "continue_stream", fmt.Sprintf("实时转码 %d%%", int(math.Round(minFloat(1, maxFloat(0, dto.Progress))*100)))
	}
	return "start_stream", "等待播放器请求转码流"
}

func (s *Server) startVideoProxySweeper() {
	go func() {
		ticker := time.NewTicker(videoProxySweepEvery)
		defer ticker.Stop()
		for range ticker.C {
			s.sweepVideoProxyCache()
		}
	}()
}

func (s *Server) sweepVideoProxyCache() {
	now := time.Now()
	var expired []*videoProxyRuntime
	s.videoProxyMu.Lock()
	for key, state := range s.videoProxyStates {
		state.LeaseUntil = videoProxyMaxSessionLease(state, now)
		if state.Queued || state.Transcoding || state.ActiveStream > 0 || state.LeaseUntil.After(now) || state.ExpiresAt.After(now) {
			continue
		}
		expired = append(expired, state)
		delete(s.videoProxyStates, key)
	}
	s.videoProxyMu.Unlock()
	for _, state := range expired {
		_ = os.Remove(state.DestPath)
		_ = os.Remove(state.TempPath)
		_ = s.db.SetAssetWorkStatus(context.Background(), state.AssetID, "video_proxy_status", model.StatusPending, nil)
	}
	s.sweepUntrackedVideoProxyFiles(now)
}

func (s *Server) sweepUntrackedVideoProxyFiles(now time.Time) {
	root := filepath.Join(s.store.CacheRoot, "video-proxies")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mp4") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < videoProxyCacheTTL {
			return nil
		}
		cacheKey := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		s.videoProxyMu.Lock()
		state := s.videoProxyStates[cacheKey]
		if state != nil {
			state.LeaseUntil = videoProxyMaxSessionLease(state, now)
		}
		active := state != nil && (state.Transcoding || state.ActiveStream > 0 || state.LeaseUntil.After(now))
		s.videoProxyMu.Unlock()
		if !active {
			_ = os.Remove(path)
		}
		return nil
	})
}

func touchFile(path string, now time.Time) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return os.Chtimes(path, now, now)
}

func assetDuration(asset model.Asset) float64 {
	if asset.Duration == nil || *asset.Duration < 0 {
		return 0
	}
	return *asset.Duration
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func videoProxyPublicError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "视频转码失败"
	}
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

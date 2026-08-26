package api

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"lpicto/backend/internal/config"
	"lpicto/backend/internal/jobs"
	"lpicto/backend/internal/model"
	videoproc "lpicto/backend/internal/video"
)

type videoSegmentRuntime struct {
	AssetID      int64
	CacheKey     string
	SessionID    string
	SessionIDs   map[string]struct{}
	SegmentIndex int
	StartSeconds float64
	Duration     float64
	DestPath     string
	TempPath     string
	Queued       bool
	Transcoding  bool
	StartedAt    time.Time
	UpdatedAt    time.Time
	Progress     float64
	SecondsDone  float64
	Bytes        int64
	Error        string
	Err          error
	Priority     int
	QueueOrder   uint64
	Claiming     bool
	DeleteOnDone bool
	Cancel       context.CancelCauseFunc
	Done         chan struct{}
}

type videoSegmentPlan struct {
	CacheKey       string
	SegmentIndex   int
	StartSeconds   float64
	Duration       float64
	TotalDuration  float64
	SegmentSeconds float64
	SegmentCount   int
}

const videoPrioritySegmentCount = 5
const videoFirstSegmentSeconds = 2.0
const videoSegmentPriorityHeader = "X-LPicto-Segment-Priority"

const (
	videoSegmentPriorityStale    = 10
	videoSegmentPriorityBalanced = 50
	videoSegmentPriorityCritical = 80
	videoSegmentPriorityPlayback = 100
)

var (
	errVideoSegmentPreempted   = errors.New("video segment preempted by playback")
	errVideoSegmentSuperseded  = errors.New("video segment superseded by a newer viewer session")
	errVideoSegmentSessionStop = errors.New("video segment session stopped")
)

func (s *Server) videoHLSPlaylist(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	if asset.BrowserPlayable {
		writeError(w, http.StatusConflict, "video_segments_not_required", "视频可直接播放，不需要转码分片")
		return
	}
	if !s.cfg.VideoProxyEnabled {
		writeError(w, http.StatusNotFound, "video_segments_disabled", "视频分片未启用")
		return
	}
	if missing, err := s.assetSourceMissing(asset); err != nil {
		if s.logger != nil {
			s.logger.Warn("check video segment source failed", "assetID", asset.ID, "relPath", asset.RelPath, "error", err)
		}
	} else if missing {
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "源文件暂时不可用")
		return
	}
	duration := assetDuration(asset)
	if duration <= 0 {
		writeError(w, http.StatusConflict, "video_duration_missing", "缺少视频时长，暂时不能分片播放")
		return
	}
	session := videoProxySessionFromRequest(r)
	priority := videoSegmentPriorityName(videoSegmentPriorityFromRequest(r, videoSegmentPriorityBalanced))
	segmentSeconds := s.adaptiveVideoSegmentSeconds(asset)
	segmentCount := videoSegmentCount(duration, segmentSeconds)
	targetDuration := int(math.Ceil(segmentSeconds))
	query := videoSegmentQuery(asset, session, priority)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "#EXTM3U")
	_, _ = fmt.Fprintln(w, "#EXT-X-VERSION:3")
	_, _ = fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", targetDuration)
	_, _ = fmt.Fprintln(w, "#EXT-X-MEDIA-SEQUENCE:0")
	_, _ = fmt.Fprintln(w, "#EXT-X-PLAYLIST-TYPE:VOD")
	for index := 0; index < segmentCount; index++ {
		if index > 0 {
			_, _ = fmt.Fprintln(w, "#EXT-X-DISCONTINUITY")
		}
		segmentDuration := videoSegmentDuration(duration, segmentSeconds, index)
		_, _ = fmt.Fprintf(w, "#EXTINF:%.3f,\n", segmentDuration)
		_, _ = fmt.Fprintf(w, "segments/%d.ts?%s\n", index, query)
	}
	_, _ = fmt.Fprintln(w, "#EXT-X-ENDLIST")
}

func (s *Server) videoHLSSegment(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	if asset.BrowserPlayable {
		writeError(w, http.StatusConflict, "video_segments_not_required", "视频可直接播放，不需要转码分片")
		return
	}
	if !s.cfg.VideoProxyEnabled {
		writeError(w, http.StatusNotFound, "video_segments_disabled", "视频分片未启用")
		return
	}
	index, err := parseVideoSegmentIndex(chi.URLParam(r, "segment"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_segment", "视频分片无效")
		return
	}
	if missing, err := s.assetSourceMissing(asset); err != nil {
		if s.logger != nil {
			s.logger.Warn("check video segment source failed", "assetID", asset.ID, "relPath", asset.RelPath, "error", err)
		}
	} else if missing {
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "源文件暂时不可用")
		return
	}
	plan, err := s.videoSegmentPlan(asset, index)
	if err != nil {
		status := http.StatusInternalServerError
		code := "video_segment_failed"
		message := "视频分片失败"
		if errors.Is(err, errVideoSegmentDurationMissing) {
			status = http.StatusConflict
			code = "video_duration_missing"
			message = "缺少视频时长，暂时不能分片播放"
		} else if errors.Is(err, errVideoSegmentOutOfRange) {
			status = http.StatusRequestedRangeNotSatisfiable
			code = "segment_out_of_range"
			message = "视频分片不存在"
		}
		writeError(w, status, code, message)
		return
	}
	session := videoProxySessionFromRequest(r)
	cacheSettings := s.videoProxyCacheSettings(r.Context())
	priority := videoSegmentPriorityFromRequest(r, videoSegmentPriorityBalanced)
	var state *videoSegmentRuntime
	for {
		var cached bool
		state, cached, err = s.ensureVideoSegmentRuntime(asset, plan, session.SessionID, cacheSettings, priority)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "video_segment_failed", "启动视频分片失败")
			return
		}
		if cached {
			break
		}
		s.markPlaybackPriority(r.Context())
		err = s.waitVideoSegment(r.Context(), state)
		if errors.Is(err, errVideoSegmentPreempted) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "video_segment_failed", "视频分片失败")
			return
		}
		break
	}
	s.serveCachedVideoSegment(w, r, state)
}

func (s *Server) videoHLSPrewarm(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	if asset.MediaType != model.MediaTypeVideo {
		writeError(w, http.StatusBadRequest, "not_video", "资源不是视频")
		return
	}
	if asset.BrowserPlayable {
		writeJSON(w, http.StatusOK, map[string]any{"cachedSegments": 0, "required": false})
		return
	}
	if !s.cfg.VideoProxyEnabled {
		writeError(w, http.StatusNotFound, "video_segments_disabled", "视频分片未启用")
		return
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("from"))
	if start < 0 {
		start = 0
	}
	if queryBool(r.URL.Query().Get("all")) {
		segmentSeconds := s.adaptiveVideoSegmentSeconds(asset)
		segmentCount := videoSegmentCount(assetDuration(asset), segmentSeconds)
		if start > segmentCount {
			start = segmentCount
		}
		started := s.startVideoSegmentFullWarm(asset, start, s.videoProxyCacheSettings(r.Context()))
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted":       true,
			"queuedSegments": segmentCount - start,
			"required":       true,
			"segmentSeconds": segmentSeconds,
			"started":        started,
		})
		return
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count < 1 || count > videoPrioritySegmentCount {
		count = videoPrioritySegmentCount
	}
	session := videoProxySessionFromRequest(r)
	cacheSettings := s.videoProxyCacheSettings(r.Context())
	priority := videoSegmentPriorityFromRequest(r, videoSegmentPriorityCritical)
	segmentSeconds := s.adaptiveVideoSegmentSeconds(asset)
	completed := 0
	for index := start; index < start+count; index++ {
		plan, err := s.videoSegmentPlan(asset, index)
		if errors.Is(err, errVideoSegmentOutOfRange) {
			break
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "video_segment_failed", "视频分片失败")
			return
		}
		state, cached, err := s.ensureVideoSegmentRuntime(asset, plan, session.SessionID, cacheSettings, priority)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "video_segment_failed", "启动视频分片失败")
			return
		}
		if !cached {
			s.markPlaybackPriority(r.Context())
			if err := s.waitVideoSegment(r.Context(), state); errors.Is(err, errVideoSegmentPreempted) {
				index--
				continue
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "video_segment_failed", "视频分片失败")
				return
			}
		}
		completed++
	}
	writeJSON(w, http.StatusOK, map[string]any{"cachedSegments": completed, "required": true, "segmentSeconds": segmentSeconds})
}

func (s *Server) startVideoSegmentFullWarm(asset model.Asset, start int, cacheSettings videoProxyCacheSettings) bool {
	key := "hls:" + asset.CacheKey
	ctx, cancel := context.WithCancel(context.Background())
	job := &videoFullWarmJob{cancel: cancel}
	if !s.registerVideoFullWarmJob(key, job) {
		cancel()
		return false
	}
	go func() {
		defer s.finishVideoFullWarmJob(key, job)
		s.warmAllVideoSegments(ctx, asset, start, cacheSettings)
	}()
	return true
}

func (s *Server) warmAllVideoSegments(ctx context.Context, asset model.Asset, start int, cacheSettings videoProxyCacheSettings) {
	stopPriority := s.holdPlaybackPriority(ctx)
	defer stopPriority()
	sessionID := fmt.Sprintf("full-cache-%d", asset.ID)
	defer s.stopVideoSegmentSession(asset.ID, sessionID)
	segmentCount := videoSegmentCount(assetDuration(asset), s.adaptiveVideoSegmentSeconds(asset))
	for index := start; index < segmentCount; index++ {
		if ctx.Err() != nil {
			return
		}
		plan, err := s.videoSegmentPlan(asset, index)
		if err != nil {
			return
		}
		state, cached, err := s.ensureVideoSegmentRuntime(asset, plan, sessionID, cacheSettings, videoSegmentPriorityBalanced)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("full video segment warmup failed to queue", "assetID", asset.ID, "segmentIndex", index, "error", err)
			}
			continue
		}
		if cached {
			continue
		}
		if err := s.waitVideoSegment(ctx, state); err != nil {
			if ctx.Err() != nil {
				return
			}
			if s.logger != nil {
				s.logger.Warn("full video segment warmup failed", "assetID", asset.ID, "segmentIndex", index, "error", err)
			}
		}
	}
}

func (s *Server) videoHLSSessionStop(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	session := videoProxySessionFromRequest(r)
	cancelled := s.stopVideoSegmentSession(asset.ID, session.SessionID)
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func (s *Server) videoHLSStatus(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.assetByParam(w, r)
	if !ok {
		return
	}
	session := videoProxySessionFromRequest(r)
	writeJSON(w, http.StatusOK, s.videoSegmentStatus(asset, session.SessionID))
}

func (s *Server) videoSegmentPlan(asset model.Asset, index int) (videoSegmentPlan, error) {
	duration := assetDuration(asset)
	if duration <= 0 {
		return videoSegmentPlan{}, errVideoSegmentDurationMissing
	}
	segmentSeconds := s.adaptiveVideoSegmentSeconds(asset)
	count := videoSegmentCount(duration, segmentSeconds)
	if index < 0 || index >= count {
		return videoSegmentPlan{}, errVideoSegmentOutOfRange
	}
	start := videoSegmentStartSeconds(segmentSeconds, index)
	segmentDuration := videoSegmentDuration(duration, segmentSeconds, index)
	return videoSegmentPlan{
		CacheKey:       videoSegmentCacheKey(asset.CacheKey, segmentSeconds, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, index),
		SegmentIndex:   index,
		StartSeconds:   start,
		Duration:       segmentDuration,
		TotalDuration:  duration,
		SegmentSeconds: segmentSeconds,
		SegmentCount:   count,
	}, nil
}

func (s *Server) ensureVideoSegmentRuntime(asset model.Asset, plan videoSegmentPlan, sessionID string, cacheSettings videoProxyCacheSettings, priority int) (*videoSegmentRuntime, bool, error) {
	dest, err := s.store.CachePath("video-proxies", plan.CacheKey, "ts")
	if err != nil {
		return nil, false, err
	}
	tmp := dest + ".tmp.ts"
	now := time.Now()
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	s.videoProxyMu.Lock()
	if s.videoSegmentStates == nil {
		s.videoSegmentStates = map[string]*videoSegmentRuntime{}
	}
	state := s.videoSegmentStates[plan.CacheKey]
	if state == nil {
		state = &videoSegmentRuntime{
			AssetID:      asset.ID,
			CacheKey:     plan.CacheKey,
			SessionID:    sessionID,
			SessionIDs:   map[string]struct{}{sessionID: {}},
			SegmentIndex: plan.SegmentIndex,
			StartSeconds: plan.StartSeconds,
			Duration:     plan.Duration,
			DestPath:     dest,
			TempPath:     tmp,
			UpdatedAt:    now,
			Done:         make(chan struct{}),
		}
		s.videoSegmentStates[plan.CacheKey] = state
	}
	videoSegmentAddSessionLocked(state, sessionID)
	state.AssetID = asset.ID
	state.CacheKey = plan.CacheKey
	state.SegmentIndex = plan.SegmentIndex
	state.StartSeconds = plan.StartSeconds
	state.Duration = plan.Duration
	state.DestPath = dest
	state.TempPath = tmp
	state.UpdatedAt = now
	if info, err := os.Stat(dest); err == nil && !info.IsDir() {
		state.Queued = false
		state.Transcoding = false
		state.Progress = 1
		state.SecondsDone = plan.Duration
		state.Bytes = info.Size()
		state.Error = ""
		state.Err = nil
		s.videoProxyMu.Unlock()
		_ = touchFile(dest, now)
		return state, true, nil
	}
	if state.Queued || state.Transcoding {
		if priority > state.Priority {
			state.Priority = priority
			state.SessionID = sessionID
			s.videoSegmentSequence++
			state.QueueOrder = s.videoSegmentSequence
		}
		s.videoProxyMu.Unlock()
		return state, false, nil
	}
	state = &videoSegmentRuntime{
		AssetID:      asset.ID,
		CacheKey:     plan.CacheKey,
		SessionID:    sessionID,
		SessionIDs:   map[string]struct{}{sessionID: {}},
		SegmentIndex: plan.SegmentIndex,
		StartSeconds: plan.StartSeconds,
		Duration:     plan.Duration,
		DestPath:     dest,
		TempPath:     tmp,
		Queued:       true,
		StartedAt:    now,
		UpdatedAt:    now,
		Priority:     priority,
	}
	s.videoSegmentSequence++
	state.QueueOrder = s.videoSegmentSequence
	state.Done = make(chan struct{})
	s.videoSegmentStates[plan.CacheKey] = state
	timeoutSeconds := math.Max(300, plan.Duration*60)
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	transcodeCtx, transcodeCancel := context.WithCancelCause(timeoutCtx)
	state.Cancel = transcodeCancel
	s.videoProxyMu.Unlock()
	go func() {
		defer timeoutCancel()
		s.runVideoSegmentTranscode(transcodeCtx, asset, plan, dest, tmp)
	}()
	return state, false, nil
}

func (s *Server) stopVideoSegmentSession(assetID int64, sessionID string) int {
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	cancelled := 0
	s.videoProxyMu.Lock()
	for key, state := range s.videoSegmentStates {
		if state == nil || state.AssetID != assetID || !videoSegmentHasSessionLocked(state, sessionID) {
			continue
		}
		videoSegmentRemoveSessionLocked(state, sessionID)
		if videoSegmentSessionCountLocked(state) > 0 {
			continue
		}
		if state.Queued || state.Claiming {
			state.DeleteOnDone = true
			if state.Cancel != nil {
				state.Cancel(errVideoSegmentSessionStop)
			}
			cancelled++
			continue
		}
		if state.Transcoding {
			// A started segment is allowed to finish so the work remains reusable
			// for the next viewer instead of being discarded mid-encode.
			continue
		}
		delete(s.videoSegmentStates, key)
	}
	s.videoProxyMu.Unlock()
	return cancelled
}

func videoSegmentAddSessionLocked(state *videoSegmentRuntime, sessionID string) {
	if state == nil {
		return
	}
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	if state.SessionIDs == nil {
		state.SessionIDs = map[string]struct{}{}
		if state.SessionID != "" {
			state.SessionIDs[sanitizeVideoProxyID(state.SessionID, "legacy")] = struct{}{}
		}
	}
	state.SessionIDs[sessionID] = struct{}{}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
}

func videoSegmentHasSessionLocked(state *videoSegmentRuntime, sessionID string) bool {
	if state == nil {
		return false
	}
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	if state.SessionIDs != nil {
		_, ok := state.SessionIDs[sessionID]
		return ok
	}
	return sanitizeVideoProxyID(state.SessionID, "legacy") == sessionID
}

func videoSegmentRemoveSessionLocked(state *videoSegmentRuntime, sessionID string) {
	if state == nil {
		return
	}
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	if state.SessionIDs == nil {
		if sanitizeVideoProxyID(state.SessionID, "legacy") == sessionID {
			state.SessionID = ""
		}
		return
	}
	delete(state.SessionIDs, sessionID)
	if state.SessionID == sessionID {
		state.SessionID = ""
		for remaining := range state.SessionIDs {
			state.SessionID = remaining
			break
		}
	}
}

func videoSegmentSessionCountLocked(state *videoSegmentRuntime) int {
	if state == nil {
		return 0
	}
	if state.SessionIDs != nil {
		return len(state.SessionIDs)
	}
	if state.SessionID != "" {
		return 1
	}
	return 0
}

func (s *Server) removeVideoSegmentCaches(assetCacheKey string) error {
	s.cancelVideoFullWarm(assetCacheKey)
	prefix := assetCacheKey + "-hls-"
	var done []<-chan struct{}
	s.videoProxyMu.Lock()
	for key, state := range s.videoSegmentStates {
		if state == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		if state.Cancel != nil && (state.Queued || state.Transcoding) {
			state.Cancel(errVideoSegmentSuperseded)
		}
		if state.Done != nil && (state.Queued || state.Transcoding) {
			done = append(done, state.Done)
		}
	}
	s.videoProxyMu.Unlock()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for _, finished := range done {
		select {
		case <-finished:
		case <-deadline.C:
			return errors.New("timed out stopping video segment transcode")
		}
	}

	s.videoProxyMu.Lock()
	for key := range s.videoSegmentStates {
		if strings.HasPrefix(key, prefix) {
			delete(s.videoSegmentStates, key)
		}
	}
	s.videoProxyMu.Unlock()
	return s.store.RemoveCachePrefix(prefix, "video-proxies", "ts")
}

func (s *Server) runVideoSegmentTranscode(ctx context.Context, asset model.Asset, plan videoSegmentPlan, dest string, tmp string) {
	releaseDest := s.cachePolicy.Pin(dest)
	defer releaseDest()
	releaseTemp := s.cachePolicy.Pin(tmp)
	defer releaseTemp()
	_ = os.Remove(tmp)
	source, err := s.store.PhotoPath(asset.RelPath)
	if err != nil {
		s.finishVideoSegmentTranscode(plan.CacheKey, dest, tmp, err)
		return
	}
	releaseSlot, err := s.acquireScheduledVideoSegmentSlot(ctx, plan.CacheKey)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			err = cause
		}
		s.finishVideoSegmentTranscode(plan.CacheKey, dest, tmp, err)
		return
	}
	defer releaseSlot()
	stopPriority := s.holdPlaybackPriority(ctx)
	defer stopPriority()
	s.markVideoSegmentTranscodeStarted(plan.CacheKey)
	canIgnoreEditList := plan.StartSeconds > 0 && videoSegmentSupportsEditListFallback(source)
	ignoreEditList := canIgnoreEditList && s.videoSegmentIgnoreEditListEnabled(asset.CacheKey)
	args := videoproc.StreamSegmentArgs(source, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, s.cfg.FFmpegHWDevice, plan.StartSeconds, plan.Duration)
	if ignoreEditList {
		args = videoproc.StreamSegmentIgnoreEditListArgs(source, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, s.cfg.FFmpegHWDevice, plan.StartSeconds, plan.Duration)
	}
	err = s.writeVideoSegment(ctx, plan, tmp, args)
	if err == nil {
		err = validateVideoSegmentFile(ctx, tmp, plan.Duration)
	}
	if err != nil && canIgnoreEditList && !ignoreEditList && ctx.Err() == nil {
		if s.logger != nil {
			s.logger.Warn("video segment fast seek produced invalid output; retrying without edit list", "assetID", asset.ID, "segmentIndex", plan.SegmentIndex, "error", err)
		}
		s.enableVideoSegmentIgnoreEditList(asset.CacheKey)
		_ = os.Remove(tmp)
		s.updateVideoSegmentProgress(plan.CacheKey, 0, plan.Duration)
		args = videoproc.StreamSegmentIgnoreEditListArgs(source, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, s.cfg.FFmpegHWDevice, plan.StartSeconds, plan.Duration)
		err = s.writeVideoSegment(ctx, plan, tmp, args)
		if err == nil {
			err = validateVideoSegmentFile(ctx, tmp, plan.Duration)
		}
	}
	if cause := context.Cause(ctx); cause != nil {
		err = cause
	}
	s.finishVideoSegmentTranscode(plan.CacheKey, dest, tmp, err)
	if err == nil {
		assetID := asset.ID
		s.cachePolicy.Register(context.Background(), "video-proxies", plan.CacheKey, dest, &assetID, 0)
	}
}

func (s *Server) acquireScheduledVideoSegmentSlot(ctx context.Context, cacheKey string) (func(), error) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		claim := false
		s.videoProxyMu.Lock()
		state := s.videoSegmentStates[cacheKey]
		if state == nil {
			s.videoProxyMu.Unlock()
			return nil, errors.New("video segment state missing")
		}
		if !state.Claiming && state.Queued && s.videoSegmentQueueHeadLocked() == state {
			state.Claiming = true
			claim = true
		}
		s.videoProxyMu.Unlock()
		if claim {
			release, err := s.acquireVideoProxySlot(ctx)
			if err != nil {
				s.videoProxyMu.Lock()
				if current := s.videoSegmentStates[cacheKey]; current == state {
					current.Claiming = false
				}
				s.videoProxyMu.Unlock()
				return nil, err
			}
			if err := context.Cause(ctx); err != nil {
				s.videoProxyMu.Lock()
				if current := s.videoSegmentStates[cacheKey]; current == state {
					current.Claiming = false
				}
				s.videoProxyMu.Unlock()
				release()
				return nil, err
			}
			return release, nil
		}
		select {
		case <-ctx.Done():
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) videoSegmentQueueHeadLocked() *videoSegmentRuntime {
	maxPriority := -1
	activeSessions := map[string]struct{}{}
	for _, state := range s.videoSegmentStates {
		if state == nil {
			continue
		}
		if state.Transcoding || state.Claiming {
			if sessionID := sanitizeVideoProxyID(state.SessionID, ""); sessionID != "" {
				activeSessions[sessionID] = struct{}{}
			}
		}
		if state.Queued && !state.Claiming && state.Cancel != nil && state.Priority > maxPriority {
			maxPriority = state.Priority
		}
	}
	var selected *videoSegmentRuntime
	var fallback *videoSegmentRuntime
	for _, state := range s.videoSegmentStates {
		if state == nil || !state.Queued || state.Claiming || state.Cancel == nil || state.Priority != maxPriority {
			continue
		}
		if fallback == nil || state.QueueOrder < fallback.QueueOrder {
			fallback = state
		}
		_, sessionBusy := activeSessions[sanitizeVideoProxyID(state.SessionID, "")]
		if !sessionBusy && (selected == nil || state.QueueOrder < selected.QueueOrder) {
			selected = state
		}
	}
	if selected != nil {
		return selected
	}
	return fallback
}

func (s *Server) markPlaybackPriority(ctx context.Context) {
	if s == nil || s.jobs == nil {
		return
	}
	if err := s.jobs.MarkPlaybackPriority(ctx, 3*time.Second); err != nil && s.logger != nil {
		s.logger.Warn("mark playback priority failed", "error", err)
	}
}

func (s *Server) holdPlaybackPriority(ctx context.Context) func() {
	if s == nil || s.jobs == nil {
		return func() {}
	}
	done := make(chan struct{})
	s.markPlaybackPriority(ctx)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				s.markPlaybackPriority(ctx)
			}
		}
	}()
	return func() {
		select {
		case <-done:
		default:
			close(done)
		}
		jobs.MarkForegroundActive(750 * time.Millisecond)
	}
}

func (s *Server) writeVideoSegment(ctx context.Context, plan videoSegmentPlan, tmp string, args []string) error {
	_ = os.Remove(tmp)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	output, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		return err
	}
	errText := make(chan string, 1)
	go func() {
		errText <- s.readVideoSegmentProgress(plan.CacheKey, plan.Duration, stderr)
	}()
	_, copyErr := io.Copy(output, stdout)
	closeErr := output.Close()
	waitErr := cmd.Wait()
	progressText := <-errText
	if copyErr != nil {
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
	return err
}

func validateVideoSegmentFile(ctx context.Context, path string, expectedDuration float64) error {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-count_packets",
		"-show_entries", "stream=nb_read_packets",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("probe video segment: %w", err)
	}
	duration, packets := parseVideoSegmentProbe(string(output))
	if !videoSegmentOutputValid(duration, packets, expectedDuration) {
		return fmt.Errorf("invalid video segment: duration=%.3f expected=%.3f packets=%d", duration, expectedDuration, packets)
	}
	return nil
}

func parseVideoSegmentProbe(output string) (float64, int64) {
	var duration float64
	var packets int64
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "duration":
			duration, _ = strconv.ParseFloat(value, 64)
		case "nb_read_packets":
			packets, _ = strconv.ParseInt(value, 10, 64)
		}
	}
	return duration, packets
}

func videoSegmentOutputValid(duration float64, packets int64, expectedDuration float64) bool {
	if packets <= 0 || duration <= 0 || expectedDuration <= 0 {
		return false
	}
	return duration >= expectedDuration*0.8
}

func videoSegmentSupportsEditListFallback(source string) bool {
	switch strings.ToLower(filepath.Ext(source)) {
	case ".mp4", ".m4v", ".mov":
		return true
	default:
		return false
	}
}

func (s *Server) videoSegmentIgnoreEditListEnabled(assetCacheKey string) bool {
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	return s.videoSegmentIgnoreEditList[assetCacheKey]
}

func (s *Server) enableVideoSegmentIgnoreEditList(assetCacheKey string) {
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	if s.videoSegmentIgnoreEditList == nil {
		s.videoSegmentIgnoreEditList = map[string]bool{}
	}
	s.videoSegmentIgnoreEditList[assetCacheKey] = true
}

func (s *Server) markVideoSegmentTranscodeStarted(cacheKey string) {
	now := time.Now()
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoSegmentStates[cacheKey]
	if state == nil {
		return
	}
	state.Queued = false
	state.Claiming = false
	state.Transcoding = true
	state.StartedAt = now
	state.UpdatedAt = now
}

func (s *Server) readVideoSegmentProgress(cacheKey string, duration float64, stderr io.Reader) string {
	var errorLines []string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "out_time_ms=") {
			ms, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_ms="), 64)
			s.updateVideoSegmentProgress(cacheKey, ms/1000000, duration)
			continue
		}
		if strings.HasPrefix(line, "out_time_us=") {
			us, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
			s.updateVideoSegmentProgress(cacheKey, us/1000000, duration)
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

func (s *Server) updateVideoSegmentProgress(cacheKey string, seconds float64, duration float64) {
	now := time.Now()
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	state := s.videoSegmentStates[cacheKey]
	if state == nil {
		return
	}
	if seconds < 0 {
		seconds = 0
	}
	state.SecondsDone = minFloat(seconds, duration)
	state.Duration = duration
	if duration > 0 {
		state.Progress = minFloat(1, maxFloat(0, seconds/duration))
	}
	state.UpdatedAt = now
}

func (s *Server) finishVideoSegmentTranscode(cacheKey string, dest string, tmp string, err error) {
	now := time.Now()
	if err != nil {
		_ = os.Remove(tmp)
	} else if renameErr := os.Rename(tmp, dest); renameErr != nil {
		err = renameErr
		_ = os.Remove(tmp)
	} else {
		_ = touchFile(dest, now)
	}
	s.videoProxyMu.Lock()
	state := s.videoSegmentStates[cacheKey]
	if state != nil {
		state.Queued = false
		state.Claiming = false
		state.Transcoding = false
		state.Cancel = nil
		state.UpdatedAt = now
		state.Err = err
		if err != nil {
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
		if state.DeleteOnDone {
			delete(s.videoSegmentStates, cacheKey)
		}
	}
	s.videoProxyMu.Unlock()
	if err != nil && !errors.Is(err, errVideoSegmentPreempted) && !errors.Is(err, errVideoSegmentSuperseded) && !errors.Is(err, errVideoSegmentSessionStop) && s.logger != nil {
		s.logger.Warn("video segment transcode failed", "cacheKey", cacheKey, "error", err)
	}
}

func parseVideoSegmentPriority(value string, fallback int) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "playback":
		return videoSegmentPriorityPlayback
	case "critical":
		return videoSegmentPriorityCritical
	case "balanced", "neighbor", "preload", "tail":
		return videoSegmentPriorityBalanced
	case "stale":
		return videoSegmentPriorityStale
	default:
		return fallback
	}
}

func videoSegmentPriorityFromRequest(r *http.Request, fallback int) int {
	if r == nil {
		return fallback
	}
	if value := strings.TrimSpace(r.Header.Get(videoSegmentPriorityHeader)); value != "" {
		return parseVideoSegmentPriority(value, fallback)
	}
	return parseVideoSegmentPriority(r.URL.Query().Get("priority"), fallback)
}

func videoSegmentPriorityName(priority int) string {
	switch {
	case priority >= videoSegmentPriorityPlayback:
		return "playback"
	case priority >= videoSegmentPriorityCritical:
		return "critical"
	case priority >= videoSegmentPriorityBalanced:
		return "preload"
	default:
		return "stale"
	}
}

func (s *Server) waitVideoSegment(ctx context.Context, state *videoSegmentRuntime) error {
	if state == nil || state.Done == nil {
		return errors.New("video segment state missing")
	}
	select {
	case <-state.Done:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	if state.Err != nil {
		return state.Err
	}
	if state.Error != "" {
		return errors.New(state.Error)
	}
	return nil
}

func (s *Server) serveCachedVideoSegment(w http.ResponseWriter, r *http.Request, state *videoSegmentRuntime) {
	releaseCache := s.cachePolicy.Pin(state.DestPath)
	defer releaseCache()
	file, err := os.Open(state.DestPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "cache_not_ready", "缓存尚未生成")
		return
	}
	s.cachePolicy.Touch(r.Context(), "video-proxies", state.CacheKey, state.DestPath)
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=1200")
	w.Header().Set("ETag", `"`+state.CacheKey+`"`)
	w.Header().Set("X-Accel-Buffering", "no")
	http.ServeContent(w, r, filepath.Base(state.DestPath), info.ModTime(), file)
}

func (s *Server) videoSegmentStatus(asset model.Asset, sessionID string) VideoSegmentStatusDTO {
	now := time.Now()
	sessionID = sanitizeVideoProxyID(sessionID, "legacy")
	cacheSummary := s.videoSegmentCacheSummary(asset)
	dto := VideoSegmentStatusDTO{
		AssetID:             asset.ID,
		SessionID:           sessionID,
		SegmentIndex:        -1,
		State:               "idle",
		Status:              "idle",
		Duration:            s.adaptiveVideoSegmentSeconds(asset),
		SegmentSeconds:      s.adaptiveVideoSegmentSeconds(asset),
		UpdatedAt:           now.Unix(),
		ServerTime:          now.Unix(),
		CachedBytes:         cacheSummary.CachedBytes,
		CachedSegments:      cacheSummary.CachedSegments,
		SegmentCount:        cacheSummary.SegmentCount,
		EstimatedTotalBytes: cacheSummary.EstimatedTotalBytes,
		SourceBytes:         asset.Size,
	}
	s.videoProxyMu.Lock()
	defer s.videoProxyMu.Unlock()
	var selected *videoSegmentRuntime
	for _, state := range s.videoSegmentStates {
		if state == nil || state.AssetID != asset.ID || (sessionID != "legacy" && !videoSegmentHasSessionLocked(state, sessionID)) {
			continue
		}
		if selected == nil || state.Priority > selected.Priority ||
			(state.Priority == selected.Priority && state.Transcoding && !selected.Transcoding) ||
			(state.Priority == selected.Priority && state.Transcoding == selected.Transcoding && state.UpdatedAt.After(selected.UpdatedAt)) {
			selected = state
		}
	}
	if selected == nil {
		dto.Message = "等待分片"
		return dto
	}
	dto.SegmentIndex = selected.SegmentIndex
	dto.Progress = selected.Progress
	dto.SecondsDone = selected.SecondsDone
	dto.Duration = selected.Duration
	dto.Bytes = selected.Bytes
	dto.Error = selected.Error
	dto.UpdatedAt = selected.UpdatedAt.Unix()
	dto.Queued = selected.Queued
	dto.Transcoding = selected.Transcoding
	if selected.Queued {
		dto.State = "queued"
		dto.Status = "queued"
		dto.Message = "等待分片转码槽位"
	} else if selected.Transcoding {
		dto.State = "transcoding"
		dto.Status = "transcoding"
		dto.Message = fmt.Sprintf("分片转码 %d%%", int(math.Round(minFloat(1, maxFloat(0, selected.Progress))*100)))
	} else if selected.Error != "" {
		dto.State = "error"
		dto.Status = "error"
		dto.Message = "分片转码失败"
	} else if selected.Progress >= 1 {
		dto.State = "cached"
		dto.Status = "cached"
		dto.Cached = true
		dto.Message = "分片已缓存"
	} else {
		dto.State = "idle"
		dto.Status = "idle"
		dto.Message = "等待分片"
	}
	return dto
}

type videoSegmentCacheSummary struct {
	CachedBytes         int64
	CachedSegments      int
	SegmentCount        int
	EstimatedTotalBytes int64
}

func (s *Server) videoSegmentCacheSummary(asset model.Asset) videoSegmentCacheSummary {
	segmentSeconds := s.adaptiveVideoSegmentSeconds(asset)
	segmentCount := videoSegmentCount(assetDuration(asset), segmentSeconds)
	summary := videoSegmentCacheSummary{SegmentCount: segmentCount}
	if segmentCount == 0 || strings.TrimSpace(asset.CacheKey) == "" {
		return summary
	}
	firstKey := videoSegmentCacheKey(asset.CacheKey, segmentSeconds, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, 0)
	firstPath, err := s.store.CacheFilePath("video-proxies", firstKey, "ts")
	if err != nil {
		return summary
	}
	entries, err := os.ReadDir(filepath.Dir(firstPath))
	if err != nil {
		return summary
	}
	entrySizes := make(map[string]int64, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ts") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		entrySizes[entry.Name()] = info.Size()
	}
	for index := 0; index < segmentCount; index++ {
		key := videoSegmentCacheKey(asset.CacheKey, segmentSeconds, s.cfg.VideoProxyMaxHeight, s.cfg.VideoProxyCRF, s.cfg.FFmpegHWAccel, index)
		if size, ok := entrySizes[key+".ts"]; ok {
			summary.CachedSegments++
			summary.CachedBytes += size
		}
	}
	if summary.CachedSegments > 0 {
		summary.EstimatedTotalBytes = int64(math.Round(float64(summary.CachedBytes) / float64(summary.CachedSegments) * float64(segmentCount)))
	}
	return summary
}

func videoSegmentQuery(asset model.Asset, session VideoProxyHeartbeatRequest, priority string) string {
	query := url.Values{}
	query.Set("v", asset.CacheKey)
	query.Set("priority", videoSegmentPriorityName(parseVideoSegmentPriority(priority, videoSegmentPriorityBalanced)))
	if session.ClientID != "" {
		query.Set("clientId", session.ClientID)
	}
	if session.SessionID != "" {
		query.Set("sessionId", session.SessionID)
	}
	return query.Encode()
}

func (s *Server) videoSegmentSeconds() int {
	if s == nil || s.cfg.VideoSegmentSeconds < 1 {
		return config.DefaultVideoSegmentSeconds
	}
	return s.cfg.VideoSegmentSeconds
}

func (s *Server) adaptiveVideoSegmentSeconds(asset model.Asset) float64 {
	duration := assetDuration(asset)
	if duration > 0 && duration <= 4 {
		return duration
	}
	maximum := float64(s.videoSegmentSeconds())
	if maximum > 4 {
		maximum = 4
	}
	if maximum < 2 {
		maximum = 2
	}
	bitrate := float64(0)
	if duration > 0 && asset.Size > 0 {
		bitrate = float64(asset.Size) * 8 / duration
	}
	width, height, fps, codec, metadataBitrate := videoSegmentMetadata(asset.MetadataJSON)
	if width <= 0 && asset.Width != nil {
		width = *asset.Width
	}
	if height <= 0 && asset.Height != nil {
		height = *asset.Height
	}
	if metadataBitrate > 0 {
		bitrate = metadataBitrate
	}
	if width <= 0 || height <= 0 {
		width, height = 1280, 720
	}
	if fps <= 0 {
		fps = 30
	}
	codecFactor := 1.0
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1":
		codecFactor = 1
	case "hevc", "h265", "vp9":
		codecFactor = 1.35
	case "av1":
		codecFactor = 1.6
	default:
		if strings.TrimSpace(codec) != "" {
			codecFactor = 1.2
		}
	}
	secondsByBytes := maximum
	if bitrate > 0 {
		secondsByBytes = float64(64<<20) * 8 / bitrate
	}
	megapixels := float64(width*height) / 1_000_000
	secondsByWork := 300 / math.Max(0.1, megapixels*fps*codecFactor)
	seconds := math.Min(maximum, math.Min(secondsByBytes, secondsByWork))
	seconds = math.Max(2, seconds)
	seconds = math.Floor(seconds*2) / 2
	if seconds < 2 {
		return 2
	}
	return seconds
}

func videoSegmentMetadata(raw *string) (width int, height int, fps float64, codec string, bitrate float64) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return
	}
	var probe struct {
		Streams []struct {
			CodecType    string `json:"codec_type"`
			CodecName    string `json:"codec_name"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			AvgFrameRate string `json:"avg_frame_rate"`
			RFrameRate   string `json:"r_frame_rate"`
			BitRate      string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			BitRate string `json:"bit_rate"`
		} `json:"format"`
	}
	if json.Unmarshal([]byte(*raw), &probe) != nil {
		return
	}
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" {
			continue
		}
		width, height, codec = stream.Width, stream.Height, stream.CodecName
		fps = parseVideoSegmentFrameRate(stream.AvgFrameRate)
		if fps <= 0 {
			fps = parseVideoSegmentFrameRate(stream.RFrameRate)
		}
		bitrate, _ = strconv.ParseFloat(stream.BitRate, 64)
		break
	}
	if formatBitrate, err := strconv.ParseFloat(probe.Format.BitRate, 64); err == nil && formatBitrate > 0 {
		bitrate = formatBitrate
	}
	return
}

func parseVideoSegmentFrameRate(value string) float64 {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) == 2 {
		numerator, _ := strconv.ParseFloat(parts[0], 64)
		denominator, _ := strconv.ParseFloat(parts[1], 64)
		if denominator > 0 {
			return numerator / denominator
		}
	}
	valueFloat, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return valueFloat
}

func videoSegmentCount(duration float64, segmentSeconds float64) int {
	if duration <= 0 || segmentSeconds <= 0 {
		return 0
	}
	first := math.Min(videoFirstSegmentSeconds, segmentSeconds)
	if duration <= first {
		return 1
	}
	return 1 + int(math.Ceil((duration-first)/segmentSeconds))
}

func videoSegmentDuration(totalDuration float64, segmentSeconds float64, index int) float64 {
	start := videoSegmentStartSeconds(segmentSeconds, index)
	if totalDuration <= start {
		return 0
	}
	target := segmentSeconds
	if index == 0 {
		target = math.Min(videoFirstSegmentSeconds, segmentSeconds)
	}
	return minFloat(target, totalDuration-start)
}

func videoSegmentStartSeconds(segmentSeconds float64, index int) float64 {
	if index <= 0 {
		return 0
	}
	first := math.Min(videoFirstSegmentSeconds, segmentSeconds)
	return first + float64(index-1)*segmentSeconds
}

func videoSegmentCacheKey(assetCacheKey string, segmentSeconds float64, maxHeight int, crf int, hwAccel string, index int) string {
	profile := fmt.Sprintf("hls-v5:%s:%d:%d:%d:%s:%d", assetCacheKey, int(math.Round(segmentSeconds*1000)), maxHeight, crf, strings.ToLower(strings.TrimSpace(hwAccel)), index)
	sum := sha1.Sum([]byte(profile))
	return fmt.Sprintf("%s-hls-%s", assetCacheKey, hex.EncodeToString(sum[:])[:16])
}

func parseVideoSegmentIndex(raw string) (int, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, ".ts"))
	if raw == "" {
		return 0, errors.New("empty segment")
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return 0, errors.New("invalid segment")
	}
	return index, nil
}

var (
	errVideoSegmentDurationMissing = errors.New("video duration missing")
	errVideoSegmentOutOfRange      = errors.New("video segment out of range")
)

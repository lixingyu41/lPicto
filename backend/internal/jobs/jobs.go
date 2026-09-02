package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"lpicto/backend/internal/debugcontrol"
)

type Task struct {
	Type         string   `json:"type"`
	AssetID      int64    `json:"assetId,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Roots        []string `json:"roots,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	Rebuild      bool     `json:"rebuild,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Attempt      int      `json:"attempt,omitempty"`
	AIGeneration uint64   `json:"aiGeneration,omitempty"`
}

type Handler func(ctx context.Context, task Task) error

const (
	imageQueueCapacity       = 131072
	videoPosterQueueCapacity = 65536
	storyboardQueueCapacity  = 65536
	redisQueuePrefix         = "lpicto:jobs:v2"
	legacyRedisQueue         = "lpicto:jobs"
	redisDedupSet            = redisQueuePrefix + ":queued"
	redisActiveHash          = redisQueuePrefix + ":active"
	redisPlaybackPriorityKey = "lpicto:playback:priority"
)

const MaxAutomaticRetries = 3

var automaticRetryDelay = func(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 10 * time.Second
	case 2:
		return time.Minute
	default:
		return 5 * time.Minute
	}
}

var (
	ErrPlaybackPriority   = errors.New("playback requires CPU priority")
	ErrMediaScanPriority  = errors.New("media scan requires highest priority")
	ErrMediaCachePriority = errors.New("media cache work requires priority over AI")
	ErrRetryable          = errors.New("task should be retried")
	ErrTaskStopped        = errors.New("task stopped by user")
)

var (
	redisControlTaskTypes  = []string{"scan_stop", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_reconcile", "scan_media", "scan_metadata", "scan_metadata_paths", "thumb_continue", "thumb_rebuild"}
	redisImageTaskTypes    = []string{"thumb", "preview"}
	redisPosterTaskTypes   = []string{"video_poster"}
	redisStoryboardTypes   = []string{"storyboard"}
	redisAITaskTypes       = []string{"ai_analyze"}
	redisMediaTaskTypes    = []string{"thumb", "preview", "video_poster", "storyboard", "video_proxy", "ai_analyze"}
	redisAllTaskTypes      = []string{"scan_stop", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_reconcile", "scan_media", "scan_metadata", "scan_metadata_paths", "thumb_continue", "thumb_rebuild", "thumb", "preview", "video_poster", "storyboard", "ai_analyze"}
	redisPromoteTaskScript = redis.NewScript(`
local items = redis.call('LRANGE', KEYS[1], 0, -1)
for _, member in ipairs(items) do
  local ok, decoded = pcall(cjson.decode, member)
  if ok and decoded.task and decoded.task.type == ARGV[1] and tostring(decoded.task.assetId or 0) == ARGV[2] then
    redis.call('LREM', KEYS[1], 1, member)
    redis.call('LPUSH', KEYS[1], ARGV[3])
    return 1
  end
end
return 0
`)
	redisPriorityEnqueueScript = redis.NewScript(`
local items = redis.call('LRANGE', KEYS[1], 0, -1)
for _, member in ipairs(items) do
  local ok, decoded = pcall(cjson.decode, member)
  local priority = 0
  if ok and decoded.task then
    priority = tonumber(decoded.task.priority or 0) or 0
  end
  if priority <= 0 or priority >= 100 then
    redis.call('LINSERT', KEYS[1], 'BEFORE', member, ARGV[1])
    return 1
  end
end
redis.call('RPUSH', KEYS[1], ARGV[1])
return 1
`)
)

type QueueStats struct {
	ImageQueued       int `json:"imageQueued"`
	ImageCap          int `json:"imageCap"`
	ThumbQueued       int `json:"thumbQueued"`
	ThumbCap          int `json:"thumbCap"`
	PreviewQueued     int `json:"previewQueued"`
	PreviewCap        int `json:"previewCap"`
	VideoPosterQueued int `json:"videoPosterQueued"`
	VideoPosterCap    int `json:"videoPosterCap"`
	VideoProxyQueued  int `json:"videoProxyQueued"`
	VideoProxyCap     int `json:"videoProxyCap"`
	VideoQueued       int `json:"videoQueued"`
	VideoCap          int `json:"videoCap"`
	ActiveThumb       int `json:"activeThumb"`
	ActivePreview     int `json:"activePreview"`
	ActiveVideoPoster int `json:"activeVideoPoster"`
	StoryboardQueued  int `json:"storyboardQueued"`
	ActiveStoryboard  int `json:"activeStoryboard"`
	ActiveTranscode   int `json:"activeTranscode"`
	AIQueued          int `json:"aiQueued"`
	ActiveAI          int `json:"activeAi"`
}

type ExecutorHealth struct {
	Queued         int
	Active         int
	BlockedReason  string
	StalledSeconds int64
	Healthy        bool
}

type WorkerConfig struct {
	Image       int
	VideoPoster int
	Storyboard  int
	AI          int
}

type Manager struct {
	imageQueue        chan Task
	videoPosterQueue  chan Task
	storyboardQueue   chan Task
	thumb             Handler
	scan              Handler
	ai                Handler
	redis             *redis.Client
	redisQueue        string
	resources         *ResourceLimiter
	logger            *slog.Logger
	mu                sync.Mutex
	queued            map[string]int
	active            map[string]int
	activeCancels     map[string]map[uint64]context.CancelCauseFunc
	activeCancelID    uint64
	aiRetryGeneration uint64
	healthMu          sync.Mutex
	stalledSince      time.Time
	wg                sync.WaitGroup
}

type queuedTask struct {
	ID   int64 `json:"id"`
	Task Task  `json:"task"`
}

func New(logger *slog.Logger, thumb Handler, policies ...ResourcePolicy) *Manager {
	var resources *ResourceLimiter
	if len(policies) > 0 {
		resources = NewResourceLimiter(policies[0])
	}
	return &Manager{
		imageQueue:        make(chan Task, imageQueueCapacity),
		videoPosterQueue:  make(chan Task, videoPosterQueueCapacity),
		storyboardQueue:   make(chan Task, storyboardQueueCapacity),
		thumb:             thumb,
		resources:         resources,
		logger:            logger,
		queued:            map[string]int{},
		active:            map[string]int{},
		activeCancels:     map[string]map[uint64]context.CancelCauseFunc{},
		aiRetryGeneration: 1,
	}
}

// CancelActive stops work that has already left the queue. The handler keeps
// the item pending, so a later manual continue can process it again.
func (m *Manager) CancelActive(taskTypes ...string) int {
	if m == nil || len(taskTypes) == 0 {
		return 0
	}
	allowed := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		allowed[taskType] = struct{}{}
	}
	m.mu.Lock()
	cancels := make([]context.CancelCauseFunc, 0)
	for taskType, active := range m.activeCancels {
		if _, ok := allowed[taskType]; !ok {
			continue
		}
		for _, cancel := range active {
			cancels = append(cancels, cancel)
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel(ErrTaskStopped)
	}
	return len(cancels)
}

func (m *Manager) registerActiveCancel(taskType string, cancel context.CancelCauseFunc) func() {
	m.mu.Lock()
	m.activeCancelID++
	id := m.activeCancelID
	if m.activeCancels[taskType] == nil {
		m.activeCancels[taskType] = make(map[uint64]context.CancelCauseFunc)
	}
	m.activeCancels[taskType][id] = cancel
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.activeCancels[taskType], id)
		if len(m.activeCancels[taskType]) == 0 {
			delete(m.activeCancels, taskType)
		}
		m.mu.Unlock()
	}
}

func NewRedis(ctx context.Context, logger *slog.Logger, redisURL string, thumb Handler, policies ...ResourcePolicy) (*Manager, error) {
	manager := New(logger, thumb, policies...)
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	manager.redis = client
	manager.redisQueue = redisQueuePrefix
	return manager, nil
}

func (m *Manager) SetScanHandler(scan Handler) {
	m.scan = scan
}

func (m *Manager) SetAIHandler(handler Handler) { m.ai = handler }

func (m *Manager) Start(ctx context.Context, cfg WorkerConfig) {
	cfg = normalizeWorkerConfig(cfg)
	if m.redis != nil {
		m.startRedisWorkers(ctx, "control", 1, redisControlTaskTypes, nil)
		m.startRedisWorkers(ctx, "image", cfg.Image, redisImageTaskTypes, nil)
		m.startRedisWorkers(ctx, "video_poster", cfg.VideoPoster, redisPosterTaskTypes, redisImageTaskTypes)
		mediaCacheTasks := append(append([]string{}, redisImageTaskTypes...), redisPosterTaskTypes...)
		m.startRedisWorkers(ctx, "storyboard", cfg.Storyboard, redisStoryboardTypes, mediaCacheTasks)
		mediaCacheTasks = append(mediaCacheTasks, redisStoryboardTypes...)
		m.startRedisWorkers(ctx, "ai", cfg.AI, redisAITaskTypes, mediaCacheTasks)
		return
	}
	for i := 0; i < cfg.Image; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "image", m.imageQueue, m.thumb)
	}
	for i := 0; i < cfg.VideoPoster; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "video_poster", m.videoPosterQueue, m.thumb)
	}
	for i := 0; i < cfg.Storyboard; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "storyboard", m.storyboardQueue, m.thumb)
	}
}

func (m *Manager) ResetRuntimeState(ctx context.Context) {
	m.resetRedisRuntimeState(ctx)
}

func (m *Manager) ClearAIQueue(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.aiRetryGeneration++
	m.mu.Unlock()
	if m.redis == nil {
		return nil
	}
	members, err := m.redis.SMembers(ctx, redisDedupSet).Result()
	if err != nil {
		return err
	}
	pipe := m.redis.Pipeline()
	pipe.Del(ctx, m.redisQueueKey("ai_analyze"))
	for _, member := range members {
		if strings.HasPrefix(member, "ai_analyze:") {
			pipe.SRem(ctx, redisDedupSet, member)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	m.queued["ai_analyze"] = 0
	m.mu.Unlock()
	return nil
}

func (m *Manager) ClearAllQueues(ctx context.Context) error {
	return m.ClearQueues(ctx, redisAllTaskTypes...)
}

// ClearQueues removes queued work of the requested types. Active work is allowed
// to finish; callers reset any stale processing state after the queue is drained.
func (m *Manager) ClearQueues(ctx context.Context, taskTypes ...string) error {
	if m == nil || len(taskTypes) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(taskTypes))
	for _, taskType := range taskTypes {
		if knownQueueTask(taskType) {
			allowed[taskType] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	if m.redis != nil {
		members, err := m.redis.SMembers(ctx, redisDedupSet).Result()
		if err != nil {
			return err
		}
		pipe := m.redis.Pipeline()
		for taskType := range allowed {
			pipe.Del(ctx, m.redisQueueKey(taskType))
		}
		for _, member := range members {
			prefix, _, _ := strings.Cut(member, ":")
			if _, ok := allowed[prefix]; ok {
				pipe.SRem(ctx, redisDedupSet, member)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	for taskType := range allowed {
		m.queued[taskType] = 0
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) MarkPlaybackPriority(ctx context.Context, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	MarkForegroundActive(ttl)
	if m == nil || m.redis == nil {
		return nil
	}
	return m.redis.Set(ctx, redisPlaybackPriorityKey, time.Now().UnixNano(), ttl).Err()
}

func (m *Manager) PlaybackPriorityActive(ctx context.Context) (bool, error) {
	if m == nil || m.redis == nil {
		return ForegroundActive(), nil
	}
	count, err := m.redis.Exists(ctx, redisPlaybackPriorityKey).Result()
	return count > 0, err
}

// BackgroundBlocker reports why background workers currently cannot start.
// It is read-only and safe for frequently-polled status endpoints.
func (m *Manager) BackgroundBlocker(ctx context.Context) string {
	if debugcontrol.BackgroundProcessingPaused() {
		return "debug_pause"
	}
	if m == nil {
		return ""
	}
	if MediaScanPriorityActive() {
		return "media_scan"
	}
	if active, err := m.PlaybackPriorityActive(ctx); err == nil && active {
		return "playback"
	}
	if active, err := m.StoryboardPriorityActive(ctx); err == nil && active {
		return "storyboard"
	}
	if m.resources != nil {
		return m.resources.blockedReason()
	}
	if ForegroundActive() {
		return "foreground"
	}
	return ""
}

func (m *Manager) ExecutorHealth(ctx context.Context) ExecutorHealth {
	if m == nil {
		return ExecutorHealth{Healthy: false}
	}
	stats := m.Stats()
	health := ExecutorHealth{
		Queued: stats.ThumbQueued + stats.PreviewQueued + stats.VideoPosterQueued + stats.StoryboardQueued + stats.AIQueued,
		Active: stats.ActiveThumb + stats.ActivePreview + stats.ActiveVideoPoster + stats.ActiveStoryboard + stats.ActiveAI,
	}
	health.BlockedReason = m.BackgroundBlocker(ctx)
	m.healthMu.Lock()
	defer m.healthMu.Unlock()
	if health.Queued == 0 || health.Active > 0 || health.BlockedReason != "" {
		m.stalledSince = time.Time{}
		health.Healthy = true
		return health
	}
	if m.stalledSince.IsZero() {
		m.stalledSince = time.Now()
	}
	health.StalledSeconds = int64(time.Since(m.stalledSince).Seconds())
	health.Healthy = health.StalledSeconds < 30
	return health
}

// StoryboardPriorityActive reports whether progress-preview generation is
// queued or running. AI yields while this NAS-reading task is active.
func (m *Manager) StoryboardPriorityActive(ctx context.Context) (bool, error) {
	if m == nil {
		return false, nil
	}
	if m.redis == nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.queued["storyboard"]+m.active["storyboard"] > 0, nil
	}
	pipe := m.redis.Pipeline()
	queued := pipe.LLen(ctx, m.redisQueueKey("storyboard"))
	active := pipe.HGet(ctx, redisActiveHash, "storyboard")
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}
	activeCount, _ := strconv.Atoi(active.Val())
	return queued.Val() > 0 || activeCount > 0, nil
}

func (m *Manager) MediaCachePriorityActive(ctx context.Context) (bool, error) {
	if m == nil {
		return false, nil
	}
	if m.redis == nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, taskType := range []string{"thumb", "preview", "video_poster", "storyboard"} {
			if m.queued[taskType]+m.active[taskType] > 0 {
				return true, nil
			}
		}
		return false, nil
	}
	return m.redisQueuesHaveBacklog(ctx, []string{"thumb", "preview", "video_poster", "storyboard"})
}

func (m *Manager) Stop() {
	m.wg.Wait()
}

func (m *Manager) startRedisWorkers(ctx context.Context, name string, workers int, taskTypes []string, blockedBy []string) {
	if workers < 1 {
		workers = 1
	}
	keys := m.redisQueueKeys(taskTypes)
	blockerKeys := m.redisQueueKeys(blockedBy)
	for i := 0; i < workers; i++ {
		m.wg.Add(1)
		go m.redisWorker(ctx, name, keys, blockerKeys, resourceManagedTypes(taskTypes))
	}
}

func (m *Manager) resetRedisRuntimeState(ctx context.Context) {
	if m.redis == nil {
		return
	}
	resetCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	keys := []string{redisActiveHash, redisDedupSet, legacyRedisQueue}
	for _, taskType := range redisMediaTaskTypes {
		keys = append(keys, m.redisQueueKey(taskType))
	}
	if err := m.redis.Del(resetCtx, keys...).Err(); err != nil && m.logger != nil {
		m.logger.Warn("reset redis job runtime state failed", "error", err)
	}
}

func (m *Manager) Stats() QueueStats {
	if m == nil {
		return QueueStats{}
	}
	m.mu.Lock()
	queued := copyCounts(m.queued)
	active := copyCounts(m.active)
	m.mu.Unlock()
	if m.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		redisQueued := m.redisQueueCounts(ctx)
		redisActive := m.redisActiveCounts(ctx)
		total := 0
		for _, count := range redisQueued {
			total += count
		}
		return QueueStats{
			ImageQueued:       total,
			ImageCap:          0,
			ThumbQueued:       redisQueued["thumb"],
			PreviewQueued:     redisQueued["preview"],
			VideoPosterQueued: redisQueued["video_poster"],
			StoryboardQueued:  redisQueued["storyboard"],
			VideoProxyQueued:  0,
			VideoProxyCap:     0,
			VideoQueued:       0,
			VideoCap:          0,
			ActiveThumb:       redisActive["thumb"],
			ActivePreview:     redisActive["preview"],
			ActiveVideoPoster: redisActive["video_poster"],
			ActiveStoryboard:  redisActive["storyboard"],
			ActiveTranscode:   redisActive["preview"],
			AIQueued:          redisQueued["ai_analyze"],
			ActiveAI:          redisActive["ai_analyze"],
		}
	}
	return QueueStats{
		ImageQueued:       len(m.imageQueue),
		ImageCap:          cap(m.imageQueue),
		ThumbQueued:       queued["thumb"],
		ThumbCap:          cap(m.imageQueue),
		PreviewQueued:     queued["preview"],
		PreviewCap:        cap(m.imageQueue),
		VideoPosterQueued: queued["video_poster"],
		StoryboardQueued:  queued["storyboard"],
		VideoPosterCap:    cap(m.videoPosterQueue),
		VideoProxyQueued:  0,
		VideoProxyCap:     0,
		VideoQueued:       0,
		VideoCap:          0,
		ActiveThumb:       active["thumb"],
		ActivePreview:     active["preview"],
		ActiveVideoPoster: active["video_poster"],
		ActiveStoryboard:  active["storyboard"],
		ActiveTranscode:   active["preview"],
	}
}

func (m *Manager) Enqueue(task Task) {
	if m == nil {
		return
	}
	if !knownQueueTask(task.Type) {
		m.logger.Warn("unknown task type", "type", task.Type, "assetID", task.AssetID)
		return
	}
	if task.Type == "ai_analyze" && task.AIGeneration == 0 {
		m.mu.Lock()
		task.AIGeneration = m.aiRetryGeneration
		m.mu.Unlock()
	}
	if m.redis != nil {
		m.enqueueRedis(task)
		return
	}
	var queue chan Task
	switch task.Type {
	case "thumb", "preview":
		queue = m.imageQueue
	case "video_poster":
		queue = m.videoPosterQueue
	case "storyboard":
		queue = m.storyboardQueue
	case "ai_analyze":
		if m.ai != nil {
			go func() {
				m.requeueAfterResult(context.Background(), task, m.runTask(context.Background(), "ai", task))
			}()
		}
		return
	}
	select {
	case queue <- task:
		m.markQueued(task.Type)
	default:
		m.logger.Warn("job queue full", "type", task.Type, "assetID", task.AssetID)
	}
}

func knownQueueTask(taskType string) bool {
	switch taskType {
	case "thumb", "preview", "video_poster", "storyboard", "ai_analyze", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_reconcile", "scan_media", "scan_metadata", "scan_metadata_paths", "thumb_continue", "thumb_rebuild", "scan_stop":
		return true
	default:
		return false
	}
}

func (m *Manager) enqueueRedis(task Task) {
	item := queuedTask{ID: time.Now().UnixNano(), Task: task}
	data, err := json.Marshal(item)
	if err != nil {
		m.logger.Warn("marshal job failed", "type", task.Type, "assetID", task.AssetID, "error", err)
		return
	}
	if !m.claimRedisDedupe(task) {
		if task.Priority > 0 && task.Priority < 100 {
			m.promoteRedisTask(task, data)
		}
		return
	}
	var pushErr error
	if task.Priority > 0 && task.Priority < 100 {
		pushErr = redisPriorityEnqueueScript.Run(context.Background(), m.redis, []string{m.redisQueueKey(task.Type)}, string(data)).Err()
	} else {
		pushErr = m.redis.RPush(context.Background(), m.redisQueueKey(task.Type), string(data)).Err()
	}
	if pushErr != nil {
		m.releaseRedisDedupe(task)
		m.logger.Warn("enqueue redis job failed", "type", task.Type, "assetID", task.AssetID, "error", pushErr)
		return
	}
	m.markQueued(task.Type)
}

func (m *Manager) promoteRedisTask(task Task, data []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisPromoteTaskScript.Run(ctx, m.redis, []string{m.redisQueueKey(task.Type)}, task.Type, strconv.FormatInt(task.AssetID, 10), string(data)).Err(); err != nil {
		m.logger.Warn("promote redis job failed", "type", task.Type, "assetID", task.AssetID, "error", err)
	}
}

func (m *Manager) redisWorker(ctx context.Context, name string, keys []string, blockedBy []string, resourceManaged bool) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if debugcontrol.BackgroundProcessingPaused() {
			if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
				return
			}
			continue
		}
		if resourceManaged && MediaScanPriorityActive() {
			if err := sleepContext(ctx, 200*time.Millisecond); err != nil {
				return
			}
			continue
		}
		if name == "ai" {
			active, err := m.PlaybackPriorityActive(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				m.logger.Warn("playback priority check failed", "worker", name, "error", err)
			} else if active {
				if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
					return
				}
				continue
			}
			storyboardActive, storyboardErr := m.StoryboardPriorityActive(ctx)
			if storyboardErr != nil {
				if ctx.Err() != nil {
					return
				}
				m.logger.Warn("storyboard priority check failed", "worker", name, "error", storyboardErr)
			} else if storyboardActive {
				if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
					return
				}
				continue
			}
		}
		if len(blockedBy) > 0 {
			blocked, err := m.redisQueuesHaveBacklog(ctx, blockedBy)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				m.logger.Warn("redis backlog check failed", "worker", name, "error", err)
			}
			if blocked {
				if err := sleepContext(ctx, 2*time.Second); err != nil {
					return
				}
				continue
			}
		}
		if resourceManaged && m.resources != nil {
			if err := m.resources.Wait(ctx); err != nil {
				return
			}
		}
		item, err := m.redis.BLPop(ctx, 5*time.Second, keys...).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.logger.Warn("redis job pop failed", "error", err)
			continue
		}
		if len(item) < 2 {
			continue
		}
		member := item[1]
		var queued queuedTask
		if err := json.Unmarshal([]byte(member), &queued); err != nil {
			m.logger.Warn("decode redis job failed", "error", err)
			continue
		}
		runErr := m.runTask(ctx, name, queued.Task)
		m.releaseRedisDedupe(queued.Task)
		m.requeueAfterResult(ctx, queued.Task, runErr)
	}
}

func (m *Manager) runTask(ctx context.Context, worker string, task Task) error {
	if task.Type != "scan_stop" && debugcontrol.BackgroundProcessingPaused() {
		return debugcontrol.ErrBackgroundProcessingPaused
	}
	handler := m.handlerFor(task.Type)
	if handler == nil {
		m.logger.Warn("unknown task type", "type", task.Type, "assetID", task.AssetID)
		return nil
	}
	taskCtx := ctx
	cancelTask := context.CancelCauseFunc(func(error) {})
	unregisterCancel := func() {}
	if resourceManagedTask(task.Type) {
		taskCtx, cancelTask = context.WithCancelCause(ctx)
		unregisterCancel = m.registerActiveCancel(task.Type, cancelTask)
		defer func() {
			unregisterCancel()
			cancelTask(nil)
		}()
	}
	stopPriorityMonitor := func() {}
	if resourceManagedTask(task.Type) {
		done := make(chan struct{})
		stopPriorityMonitor = func() {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		go m.cancelForHigherPriorityWork(taskCtx, task.Type, cancelTask, done)
		defer stopPriorityMonitor()
	}
	release := func() {}
	if resourceManagedTask(task.Type) && m.resources != nil {
		var err error
		release, err = m.resources.Acquire(taskCtx)
		if err != nil {
			return context.Cause(taskCtx)
		}
	}
	m.markStarted(task.Type)
	err := handler(taskCtx, task)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrMediaScanPriority) && !errors.Is(err, ErrMediaCachePriority) && !errors.Is(err, ErrPlaybackPriority) && !errors.Is(err, ErrTaskStopped) {
		m.logger.Warn("job failed", "worker", worker, "type", task.Type, "assetID", task.AssetID, "error", err)
	}
	release()
	m.markDone(task.Type)
	return err
}

func (m *Manager) cancelForHigherPriorityWork(ctx context.Context, taskType string, cancel context.CancelCauseFunc, done <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if MediaScanPriorityActive() {
			cancel(ErrMediaScanPriority)
			return
		}
		active, err := m.PlaybackPriorityActive(ctx)
		if err == nil && active {
			cancel(ErrPlaybackPriority)
			return
		}
		if taskType == "ai_analyze" {
			mediaCacheActive, mediaCacheErr := m.MediaCachePriorityActive(ctx)
			if mediaCacheErr == nil && mediaCacheActive {
				cancel(ErrMediaCachePriority)
				return
			}
			storyboardActive, storyboardErr := m.StoryboardPriorityActive(ctx)
			if storyboardErr == nil && storyboardActive {
				cancel(ErrPlaybackPriority)
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) redisQueueKey(taskType string) string {
	return m.redisQueue + ":" + taskType
}

func (m *Manager) redisQueueKeys(taskTypes []string) []string {
	keys := make([]string, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		keys = append(keys, m.redisQueueKey(taskType))
	}
	return keys
}

func (m *Manager) redisQueueCounts(ctx context.Context) map[string]int {
	counts := make(map[string]int, len(redisAllTaskTypes))
	pipe := m.redis.Pipeline()
	cmds := make(map[string]*redis.IntCmd, len(redisAllTaskTypes))
	for _, taskType := range redisAllTaskTypes {
		cmds[taskType] = pipe.LLen(ctx, m.redisQueueKey(taskType))
	}
	_, _ = pipe.Exec(ctx)
	for taskType, cmd := range cmds {
		counts[taskType] = int(cmd.Val())
	}
	return counts
}

func (m *Manager) redisQueuesHaveBacklog(ctx context.Context, keys []string) (bool, error) {
	if len(keys) == 0 {
		return false, nil
	}
	pipe := m.redis.Pipeline()
	cmds := make([]*redis.IntCmd, 0, len(keys))
	for _, key := range keys {
		cmds = append(cmds, pipe.LLen(ctx, key))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}
	for _, cmd := range cmds {
		if cmd.Val() > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) redisActiveCounts(ctx context.Context) map[string]int {
	counts := make(map[string]int, len(redisMediaTaskTypes))
	values, err := m.redis.HGetAll(ctx, redisActiveHash).Result()
	if err != nil {
		return counts
	}
	for taskType, raw := range values {
		count, err := strconv.Atoi(raw)
		if err == nil && count > 0 {
			counts[taskType] = count
		}
	}
	return counts
}

func (m *Manager) claimRedisDedupe(task Task) bool {
	key := redisDedupeKey(task)
	if key == "" {
		return true
	}
	claimed, err := m.redis.SAdd(context.Background(), redisDedupSet, key).Result()
	if err != nil {
		m.logger.Warn("claim redis job failed", "type", task.Type, "assetID", task.AssetID, "error", err)
		return false
	}
	return claimed > 0
}

func (m *Manager) releaseRedisDedupe(task Task) {
	key := redisDedupeKey(task)
	if key == "" || m.redis == nil {
		return
	}
	if err := m.redis.SRem(context.Background(), redisDedupSet, key).Err(); err != nil && m.logger != nil {
		m.logger.Warn("release redis job failed", "type", task.Type, "assetID", task.AssetID, "error", err)
	}
}

func redisDedupeKey(task Task) string {
	switch task.Type {
	case "scan_count", "scan_reconcile", "scan_media", "scan_metadata", "thumb_continue", "thumb_rebuild":
		return task.Type + ":" + strings.Join(task.Roots, "\x00")
	case "scan_metadata_paths":
		return task.Type + ":" + strings.Join(task.Paths, "\x00")
	}
	if task.AssetID <= 0 || !resourceManagedTask(task.Type) {
		return ""
	}
	return task.Type + ":" + strconv.FormatInt(task.AssetID, 10)
}

func resourceManagedTypes(taskTypes []string) bool {
	for _, taskType := range taskTypes {
		if resourceManagedTask(taskType) {
			return true
		}
	}
	return false
}

func resourceManagedTask(taskType string) bool {
	switch taskType {
	case "thumb", "preview", "video_poster", "storyboard", "ai_analyze":
		return true
	default:
		return false
	}
}

func (m *Manager) requeueAfterResult(ctx context.Context, task Task, runErr error) {
	if runErr == nil || errors.Is(runErr, ErrTaskStopped) || errors.Is(runErr, context.Canceled) {
		return
	}
	if errors.Is(runErr, ErrMediaScanPriority) || errors.Is(runErr, ErrMediaCachePriority) || errors.Is(runErr, ErrPlaybackPriority) || errors.Is(runErr, debugcontrol.ErrBackgroundProcessingPaused) {
		m.Enqueue(task)
		return
	}
	if task.Type == "ai_analyze" && !errors.Is(runErr, ErrRetryable) {
		return
	}
	if task.Type == "ai_analyze" && task.Priority == 1 {
		return
	}
	if !errors.Is(runErr, ErrRetryable) && !resourceManagedTask(task.Type) {
		return
	}
	if task.Attempt >= MaxAutomaticRetries {
		return
	}
	task.Attempt++
	delay := automaticRetryDelay(task.Attempt)
	if task.Type == "ai_analyze" {
		m.mu.Lock()
		stale := task.AIGeneration != m.aiRetryGeneration
		m.mu.Unlock()
		if stale {
			return
		}
	}
	if m.logger != nil {
		m.logger.Info("scheduled automatic task retry", "type", task.Type, "assetID", task.AssetID, "attempt", task.Attempt, "delay", delay)
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if task.Type == "ai_analyze" {
				m.mu.Lock()
				stale := task.AIGeneration != m.aiRetryGeneration
				m.mu.Unlock()
				if stale {
					return
				}
			}
			m.Enqueue(task)
		}
	}()
}

func (m *Manager) handlerFor(taskType string) Handler {
	switch taskType {
	case "thumb", "preview", "video_poster", "storyboard":
		return m.thumb
	case "ai_analyze":
		return m.ai
	case "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_reconcile", "scan_media", "scan_metadata", "scan_metadata_paths", "thumb_continue", "thumb_rebuild", "scan_stop":
		return m.scan
	default:
		return nil
	}
}

func (m *Manager) worker(ctx context.Context, name string, queue <-chan Task, handler Handler) {
	_ = handler
	defer m.wg.Done()
	for {
		if debugcontrol.BackgroundProcessingPaused() {
			if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case task := <-queue:
			m.requeueAfterResult(ctx, task, m.runTask(ctx, name, task))
		}
	}
}

func (m *Manager) markQueued(taskType string) {
	m.mu.Lock()
	m.queued[taskType]++
	m.mu.Unlock()
}

func (m *Manager) markStarted(taskType string) {
	m.mu.Lock()
	if m.queued[taskType] > 0 {
		m.queued[taskType]--
	}
	m.active[taskType]++
	m.mu.Unlock()
	m.redisIncrementActive(taskType, 1)
}

func (m *Manager) markDone(taskType string) {
	m.mu.Lock()
	if m.active[taskType] > 0 {
		m.active[taskType]--
	}
	m.mu.Unlock()
	m.redisIncrementActive(taskType, -1)
}

func (m *Manager) redisIncrementActive(taskType string, delta int64) {
	if m == nil || m.redis == nil || !resourceManagedTask(taskType) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	count, err := m.redis.HIncrBy(ctx, redisActiveHash, taskType, delta).Result()
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("update redis active job count failed", "type", taskType, "error", err)
		}
		return
	}
	if count <= 0 {
		_ = m.redis.HDel(ctx, redisActiveHash, taskType).Err()
	}
}

func copyCounts(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func normalizeWorkerConfig(cfg WorkerConfig) WorkerConfig {
	if cfg.Image < 1 {
		cfg.Image = 1
	}
	if cfg.VideoPoster < 1 {
		cfg.VideoPoster = 1
	}
	if cfg.Storyboard < 1 {
		cfg.Storyboard = 1
	}
	return cfg
}

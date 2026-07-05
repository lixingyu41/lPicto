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
)

type Task struct {
	Type     string   `json:"type"`
	AssetID  int64    `json:"assetId,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Roots    []string `json:"roots,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Rebuild  bool     `json:"rebuild,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

type Handler func(ctx context.Context, task Task) error

const (
	imageQueueCapacity       = 131072
	videoPosterQueueCapacity = 65536
	redisQueuePrefix         = "lpicto:jobs:v2"
	legacyRedisQueue         = "lpicto:jobs"
	redisDedupSet            = redisQueuePrefix + ":queued"
	redisActiveHash          = redisQueuePrefix + ":active"
)

var (
	redisControlTaskTypes = []string{"scan_stop", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_metadata", "scan_metadata_paths", "thumb_rebuild"}
	redisImageTaskTypes   = []string{"thumb", "preview"}
	redisPosterTaskTypes  = []string{"video_poster"}
	redisMediaTaskTypes   = []string{"thumb", "preview", "video_poster", "video_proxy"}
	redisAllTaskTypes     = []string{"scan_stop", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_metadata", "scan_metadata_paths", "thumb_rebuild", "thumb", "preview", "video_poster"}
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
	ActiveTranscode   int `json:"activeTranscode"`
}

type WorkerConfig struct {
	Image       int
	VideoPoster int
}

type Manager struct {
	imageQueue       chan Task
	videoPosterQueue chan Task
	thumb            Handler
	scan             Handler
	redis            *redis.Client
	redisQueue       string
	resources        *ResourceLimiter
	logger           *slog.Logger
	mu               sync.Mutex
	queued           map[string]int
	active           map[string]int
	wg               sync.WaitGroup
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
		imageQueue:       make(chan Task, imageQueueCapacity),
		videoPosterQueue: make(chan Task, videoPosterQueueCapacity),
		thumb:            thumb,
		resources:        resources,
		logger:           logger,
		queued:           map[string]int{},
		active:           map[string]int{},
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

func (m *Manager) Start(ctx context.Context, cfg WorkerConfig) {
	cfg = normalizeWorkerConfig(cfg)
	if m.redis != nil {
		m.startRedisWorkers(ctx, "control", 1, redisControlTaskTypes, nil)
		m.startRedisWorkers(ctx, "image", cfg.Image, redisImageTaskTypes, nil)
		m.startRedisWorkers(ctx, "video_poster", cfg.VideoPoster, redisPosterTaskTypes, redisImageTaskTypes)
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
}

func (m *Manager) ResetRuntimeState(ctx context.Context) {
	m.resetRedisRuntimeState(ctx)
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
			VideoProxyQueued:  0,
			VideoProxyCap:     0,
			VideoQueued:       0,
			VideoCap:          0,
			ActiveThumb:       redisActive["thumb"] + redisActive["video_poster"],
			ActiveTranscode:   redisActive["preview"],
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
		VideoPosterCap:    cap(m.videoPosterQueue),
		VideoProxyQueued:  0,
		VideoProxyCap:     0,
		VideoQueued:       0,
		VideoCap:          0,
		ActiveThumb:       active["thumb"] + active["video_poster"],
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
	case "thumb", "preview", "video_poster", "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_metadata", "scan_metadata_paths", "thumb_rebuild", "scan_stop":
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
		return
	}
	if err := m.redis.RPush(context.Background(), m.redisQueueKey(task.Type), string(data)).Err(); err != nil {
		m.releaseRedisDedupe(task)
		m.logger.Warn("enqueue redis job failed", "type", task.Type, "assetID", task.AssetID, "error", err)
		return
	}
	m.markQueued(task.Type)
}

func (m *Manager) redisWorker(ctx context.Context, name string, keys []string, blockedBy []string, resourceManaged bool) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
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
		m.runTask(ctx, name, queued.Task)
		m.releaseRedisDedupe(queued.Task)
	}
}

func (m *Manager) runTask(ctx context.Context, worker string, task Task) {
	handler := m.handlerFor(task.Type)
	if handler == nil {
		m.logger.Warn("unknown task type", "type", task.Type, "assetID", task.AssetID)
		return
	}
	release := func() {}
	if resourceManagedTask(task.Type) && m.resources != nil {
		var err error
		release, err = m.resources.Acquire(ctx)
		if err != nil {
			return
		}
	}
	m.markStarted(task.Type)
	if err := handler(ctx, task); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("job failed", "worker", worker, "type", task.Type, "assetID", task.AssetID, "error", err)
	}
	release()
	m.markDone(task.Type)
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
	case "scan_count", "scan_metadata", "thumb_rebuild":
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
	case "thumb", "preview", "video_poster":
		return true
	default:
		return false
	}
}

func (m *Manager) handlerFor(taskType string) Handler {
	switch taskType {
	case "thumb", "preview", "video_poster":
		return m.thumb
	case "scan", "scan_roots", "scan_rebuild", "scan_count", "scan_metadata", "scan_metadata_paths", "thumb_rebuild", "scan_stop":
		return m.scan
	default:
		return nil
	}
}

func (m *Manager) worker(ctx context.Context, name string, queue <-chan Task, handler Handler) {
	_ = handler
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-queue:
			m.runTask(ctx, name, task)
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
	return cfg
}

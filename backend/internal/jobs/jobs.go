package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type Task struct {
	Type    string
	AssetID int64
}

type Handler func(ctx context.Context, task Task) error

const (
	imageQueueCapacity       = 131072
	videoPosterQueueCapacity = 65536
	videoProxyQueueCapacity  = 8192
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
	VideoProxy  int
}

type Manager struct {
	imageQueue       chan Task
	videoPosterQueue chan Task
	videoProxyQueue  chan Task
	thumb            Handler
	video            Handler
	resources        *ResourceLimiter
	logger           *slog.Logger
	mu               sync.Mutex
	queued           map[string]int
	active           map[string]int
	wg               sync.WaitGroup
}

func New(logger *slog.Logger, thumb Handler, video Handler, policies ...ResourcePolicy) *Manager {
	var resources *ResourceLimiter
	if len(policies) > 0 {
		resources = NewResourceLimiter(policies[0])
	}
	return &Manager{
		imageQueue:       make(chan Task, imageQueueCapacity),
		videoPosterQueue: make(chan Task, videoPosterQueueCapacity),
		videoProxyQueue:  make(chan Task, videoProxyQueueCapacity),
		thumb:            thumb,
		video:            video,
		resources:        resources,
		logger:           logger,
		queued:           map[string]int{},
		active:           map[string]int{},
	}
}

func (m *Manager) Start(ctx context.Context, cfg WorkerConfig) {
	cfg = normalizeWorkerConfig(cfg)
	for i := 0; i < cfg.Image; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "image", m.imageQueue, m.thumb)
	}
	for i := 0; i < cfg.VideoPoster; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "video_poster", m.videoPosterQueue, m.thumb)
	}
	for i := 0; i < cfg.VideoProxy; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "video_proxy", m.videoProxyQueue, m.video)
	}
}

func (m *Manager) Stop() {
	m.wg.Wait()
}

func (m *Manager) Stats() QueueStats {
	if m == nil {
		return QueueStats{}
	}
	m.mu.Lock()
	queued := copyCounts(m.queued)
	active := copyCounts(m.active)
	m.mu.Unlock()
	videoQueued := queued["video_proxy"]
	videoCap := cap(m.videoProxyQueue)
	return QueueStats{
		ImageQueued:       len(m.imageQueue),
		ImageCap:          cap(m.imageQueue),
		ThumbQueued:       queued["thumb"],
		ThumbCap:          cap(m.imageQueue),
		PreviewQueued:     queued["preview"],
		PreviewCap:        cap(m.imageQueue),
		VideoPosterQueued: queued["video_poster"],
		VideoPosterCap:    cap(m.videoPosterQueue),
		VideoProxyQueued:  queued["video_proxy"],
		VideoProxyCap:     cap(m.videoProxyQueue),
		VideoQueued:       videoQueued,
		VideoCap:          videoCap,
		ActiveThumb:       active["thumb"] + active["video_poster"],
		ActiveTranscode:   active["preview"] + active["video_proxy"],
	}
}

func (m *Manager) Enqueue(task Task) {
	var queue chan Task
	switch task.Type {
	case "thumb", "preview":
		queue = m.imageQueue
	case "video_poster":
		queue = m.videoPosterQueue
	case "video_proxy":
		queue = m.videoProxyQueue
	default:
		m.logger.Warn("unknown task type", "type", task.Type, "assetID", task.AssetID)
		return
	}
	select {
	case queue <- task:
		m.markQueued(task.Type)
	default:
		m.logger.Warn("job queue full", "type", task.Type, "assetID", task.AssetID)
	}
}

func (m *Manager) worker(ctx context.Context, name string, queue <-chan Task, handler Handler) {
	defer m.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-queue:
			m.markStarted(task.Type)
			release, err := m.resources.Acquire(ctx)
			if err != nil {
				m.markDone(task.Type)
				return
			}
			if err := handler(ctx, task); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("job failed", "worker", name, "type", task.Type, "assetID", task.AssetID, "error", err)
			}
			release()
			m.markDone(task.Type)
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
}

func (m *Manager) markDone(taskType string) {
	m.mu.Lock()
	if m.active[taskType] > 0 {
		m.active[taskType]--
	}
	m.mu.Unlock()
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
	if cfg.VideoProxy < 1 {
		cfg.VideoProxy = 1
	}
	return cfg
}

package jobs

import (
	"context"
	"log/slog"
	"sync"
)

type Task struct {
	Type    string
	AssetID int64
}

type Handler func(ctx context.Context, task Task) error

const (
	thumbQueueCapacity = 131072
	videoQueueCapacity = 32768
)

type QueueStats struct {
	ThumbQueued int `json:"thumbQueued"`
	ThumbCap    int `json:"thumbCap"`
	VideoQueued int `json:"videoQueued"`
	VideoCap    int `json:"videoCap"`
}

type Manager struct {
	thumbQueue chan Task
	videoQueue chan Task
	thumb      Handler
	video      Handler
	logger     *slog.Logger
	wg         sync.WaitGroup
}

func New(logger *slog.Logger, thumb Handler, video Handler) *Manager {
	return &Manager{
		thumbQueue: make(chan Task, thumbQueueCapacity),
		videoQueue: make(chan Task, videoQueueCapacity),
		thumb:      thumb,
		video:      video,
		logger:     logger,
	}
}

func (m *Manager) Start(ctx context.Context, thumbWorkers int, videoWorkers int) {
	for i := 0; i < thumbWorkers; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "thumb", m.thumbQueue, m.thumb)
	}
	for i := 0; i < videoWorkers; i++ {
		m.wg.Add(1)
		go m.worker(ctx, "video", m.videoQueue, m.video)
	}
}

func (m *Manager) Stop() {
	m.wg.Wait()
}

func (m *Manager) Stats() QueueStats {
	if m == nil {
		return QueueStats{}
	}
	return QueueStats{
		ThumbQueued: len(m.thumbQueue),
		ThumbCap:    cap(m.thumbQueue),
		VideoQueued: len(m.videoQueue),
		VideoCap:    cap(m.videoQueue),
	}
}

func (m *Manager) Enqueue(task Task) {
	var queue chan Task
	switch task.Type {
	case "thumb", "preview":
		queue = m.thumbQueue
	case "video_poster", "video_proxy":
		queue = m.videoQueue
	default:
		m.logger.Warn("unknown task type", "type", task.Type, "assetID", task.AssetID)
		return
	}
	select {
	case queue <- task:
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
			if err := handler(ctx, task); err != nil {
				m.logger.Warn("job failed", "worker", name, "type", task.Type, "assetID", task.AssetID, "error", err)
			}
		}
	}
}

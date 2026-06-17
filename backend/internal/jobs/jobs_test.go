package jobs

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestVideoPosterQueueHasWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	handled := make(chan Task, 1)
	videoHandled := make(chan Task, 1)
	manager := New(slog.Default(), func(ctx context.Context, task Task) error {
		handled <- task
		return nil
	}, func(ctx context.Context, task Task) error {
		videoHandled <- task
		return nil
	})
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1, VideoProxy: 1})
	defer func() {
		cancel()
		manager.Stop()
	}()

	manager.Enqueue(Task{Type: "video_poster", AssetID: 42})

	select {
	case task := <-handled:
		if task.Type != "video_poster" || task.AssetID != 42 {
			t.Fatalf("handled task = %#v", task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("video_poster task was not consumed")
	}

	select {
	case task := <-videoHandled:
		t.Fatalf("video handler received %#v", task)
	default:
	}
}

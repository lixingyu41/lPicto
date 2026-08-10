package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelActiveStopsRunningMediaTask(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan error, 1)
	manager := New(slog.Default(), func(ctx context.Context, _ Task) error {
		close(started)
		<-ctx.Done()
		cause := context.Cause(ctx)
		stopped <- cause
		return cause
	})

	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1})
	defer func() {
		cancel()
		manager.Stop()
	}()
	manager.Enqueue(Task{Type: "storyboard", AssetID: 42})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("storyboard task did not start")
	}
	if count := manager.CancelActive("storyboard"); count != 1 {
		t.Fatalf("canceled active tasks = %d, want 1", count)
	}
	select {
	case cause := <-stopped:
		if !errors.Is(cause, ErrTaskStopped) {
			t.Fatalf("stop cause = %v, want ErrTaskStopped", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active storyboard task did not stop")
	}
}

func TestVideoPosterQueueHasWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	handled := make(chan Task, 1)
	manager := New(slog.Default(), func(ctx context.Context, task Task) error {
		handled <- task
		return nil
	})
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1})
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
}

func TestAIQueueHasSingleWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handled := make(chan Task, 1)
	manager := New(slog.Default(), func(context.Context, Task) error { return nil })
	manager.SetAIHandler(func(_ context.Context, task Task) error { handled <- task; return nil })
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1, AI: 1})
	defer func() { cancel(); manager.Stop() }()
	manager.Enqueue(Task{Type: "ai_analyze", AssetID: 7})
	select {
	case task := <-handled:
		if task.AssetID != 7 {
			t.Fatalf("asset=%d", task.AssetID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AI task was not consumed")
	}
}

func TestAIQueueRetriesRetryableFailureOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	done := make(chan struct{})
	manager := New(slog.Default(), func(context.Context, Task) error { return nil })
	manager.SetAIHandler(func(_ context.Context, _ Task) error {
		if attempts.Add(1) == 1 {
			return ErrRetryable
		}
		close(done)
		return nil
	})
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1, AI: 1})
	defer func() {
		cancel()
		manager.Stop()
	}()

	manager.Enqueue(Task{Type: "ai_analyze", AssetID: 8})
	select {
	case <-done:
		if attempts.Load() != 2 {
			t.Fatalf("attempts = %d, want 2", attempts.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retryable AI task was not retried")
	}
}

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

func TestMediaScanPriorityPreemptsActiveAI(t *testing.T) {
	started := make(chan struct{})
	manager := New(slog.Default(), func(context.Context, Task) error { return nil })
	manager.SetAIHandler(func(ctx context.Context, _ Task) error {
		close(started)
		<-ctx.Done()
		return context.Cause(ctx)
	})
	result := make(chan error, 1)
	go func() { result <- manager.runTask(context.Background(), "ai", Task{Type: "ai_analyze", AssetID: 9}) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("AI task did not start")
	}
	release := EnterMediaScanPriority()
	defer release()
	select {
	case err := <-result:
		if !errors.Is(err, ErrMediaScanPriority) {
			t.Fatalf("AI stop cause = %v, want ErrMediaScanPriority", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active AI task did not yield to media scan")
	}
}

func TestMediaCacheWorkPreemptsActiveAI(t *testing.T) {
	started := make(chan struct{})
	manager := New(slog.Default(), func(context.Context, Task) error { return nil })
	manager.SetAIHandler(func(ctx context.Context, _ Task) error {
		close(started)
		<-ctx.Done()
		return context.Cause(ctx)
	})
	result := make(chan error, 1)
	go func() { result <- manager.runTask(context.Background(), "ai", Task{Type: "ai_analyze", AssetID: 10}) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("AI task did not start")
	}
	manager.Enqueue(Task{Type: "thumb", AssetID: 11})
	select {
	case err := <-result:
		if !errors.Is(err, ErrMediaCachePriority) {
			t.Fatalf("AI stop cause = %v, want ErrMediaCachePriority", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active AI task did not yield to media cache work")
	}
}

func TestMediaScanIsReportedBeforePlayback(t *testing.T) {
	manager := New(slog.Default(), nil)
	release := EnterMediaScanPriority()
	defer release()
	MarkForegroundActive(time.Second)
	if reason := manager.BackgroundBlocker(context.Background()); reason != "media_scan" {
		t.Fatalf("blocker = %q, want media_scan", reason)
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
	previousRetryDelay := automaticRetryDelay
	automaticRetryDelay = func(int) time.Duration { return 10 * time.Millisecond }
	defer func() { automaticRetryDelay = previousRetryDelay }()

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

func TestAutomaticRetryStopsAfterThreeRetries(t *testing.T) {
	previousRetryDelay := automaticRetryDelay
	automaticRetryDelay = func(int) time.Duration { return 5 * time.Millisecond }
	defer func() { automaticRetryDelay = previousRetryDelay }()

	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	done := make(chan struct{})
	manager := New(slog.Default(), func(context.Context, Task) error {
		if attempts.Add(1) == MaxAutomaticRetries+1 {
			close(done)
		}
		return ErrRetryable
	})
	manager.Start(ctx, WorkerConfig{Image: 1, VideoPoster: 1})
	defer func() {
		cancel()
		manager.Stop()
	}()
	manager.Enqueue(Task{Type: "thumb", AssetID: 19})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("automatic retries did not reach the final attempt")
	}
	time.Sleep(40 * time.Millisecond)
	if got := attempts.Load(); got != MaxAutomaticRetries+1 {
		t.Fatalf("attempts = %d, want %d", got, MaxAutomaticRetries+1)
	}
}

func TestExecutorHealthDistinguishesWaitingFromStalled(t *testing.T) {
	atomic.StoreInt64(&foregroundActive, 0)
	foregroundUntil.Store(0)
	defer foregroundUntil.Store(0)
	manager := New(slog.Default(), func(context.Context, Task) error { return nil })
	manager.Enqueue(Task{Type: "thumb", AssetID: 23})
	if health := manager.ExecutorHealth(context.Background()); !health.Healthy || health.Queued != 1 {
		t.Fatalf("initial executor health = %#v", health)
	}
	manager.healthMu.Lock()
	manager.stalledSince = time.Now().Add(-31 * time.Second)
	manager.healthMu.Unlock()
	if health := manager.ExecutorHealth(context.Background()); health.Healthy || health.StalledSeconds < 30 {
		t.Fatalf("stalled executor health = %#v", health)
	}
	MarkForegroundActive(time.Second)
	if health := manager.ExecutorHealth(context.Background()); !health.Healthy || health.BlockedReason != "playback" {
		t.Fatalf("playback-blocked executor health = %#v", health)
	}
}

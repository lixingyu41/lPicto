package api

import (
	"context"
	"testing"
	"time"
)

func TestMediaIOSchedulerUsesPriorityThenFIFO(t *testing.T) {
	scheduler := &mediaIOScheduler{}
	releaseActive, err := scheduler.acquire(context.Background(), mediaIOPriorityCurrent)
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 4)
	start := func(name string, priority mediaIOPriority) {
		go func() {
			release, acquireErr := scheduler.acquire(context.Background(), priority)
			if acquireErr != nil {
				order <- "error"
				return
			}
			order <- name
			release()
		}()
	}
	waitForQueued := func(want int) {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			scheduler.mu.Lock()
			queued := len(scheduler.waiters)
			scheduler.mu.Unlock()
			if queued == want {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("queued tasks did not reach %d", want)
	}
	start("preload-1", mediaIOPriorityPreload)
	waitForQueued(1)
	start("preload-2", mediaIOPriorityPreload)
	waitForQueued(2)
	start("full-warm", mediaIOPriorityFullWarm)
	waitForQueued(3)
	start("current", mediaIOPriorityCurrent)
	waitForQueued(4)
	releaseActive()
	for index, want := range []string{"current", "full-warm", "preload-1", "preload-2"} {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("order[%d] = %q, want %q", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for order[%d]", index)
		}
	}
}

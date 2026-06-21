package jobs

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ResourcePolicy struct {
	MaxActive          int
	LoadTarget         float64
	MinFreeMemoryBytes uint64
	CheckInterval      time.Duration
	StartSpacing       time.Duration
}

type ResourceLimiter struct {
	policy    ResourcePolicy
	sem       chan struct{}
	mu        sync.Mutex
	lastStart time.Time
}

var foregroundActive int64
var foregroundUntil atomic.Int64

func EnterForeground() func() {
	atomic.AddInt64(&foregroundActive, 1)
	return func() {
		atomic.AddInt64(&foregroundActive, -1)
		MarkForegroundActive(750 * time.Millisecond)
	}
}

func MarkForegroundActive(duration time.Duration) {
	if duration <= 0 {
		return
	}
	until := time.Now().Add(duration).UnixNano()
	for {
		current := foregroundUntil.Load()
		if until <= current || foregroundUntil.CompareAndSwap(current, until) {
			return
		}
	}
}

func ForegroundActive() bool {
	if atomic.LoadInt64(&foregroundActive) > 0 {
		return true
	}
	return time.Now().UnixNano() < foregroundUntil.Load()
}

func NewResourceLimiter(policy ResourcePolicy) *ResourceLimiter {
	if policy.MaxActive < 1 {
		policy.MaxActive = 1
	}
	if policy.CheckInterval <= 0 {
		policy.CheckInterval = 500 * time.Millisecond
	}
	if policy.StartSpacing <= 0 {
		policy.StartSpacing = 50 * time.Millisecond
	}
	return &ResourceLimiter{policy: policy, sem: make(chan struct{}, policy.MaxActive)}
}

func (l *ResourceLimiter) Acquire(ctx context.Context) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	released := false
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true
		<-l.sem
	}
	for {
		if err := l.waitForStartSpacing(ctx); err != nil {
			return nil, err
		}
		if !l.canStart() {
			if err := sleepContext(ctx, l.policy.CheckInterval); err != nil {
				return nil, err
			}
			continue
		}
		select {
		case l.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if l.canStart() {
			l.markStarted()
			return release, nil
		}
		release()
		if err := sleepContext(ctx, l.policy.CheckInterval); err != nil {
			return nil, err
		}
	}
}

func (l *ResourceLimiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		if l.canStart() {
			return nil
		}
		if err := sleepContext(ctx, l.policy.CheckInterval); err != nil {
			return err
		}
	}
}

func (l *ResourceLimiter) waitForStartSpacing(ctx context.Context) error {
	for {
		l.mu.Lock()
		wait := l.policy.StartSpacing - time.Since(l.lastStart)
		l.mu.Unlock()
		if wait <= 0 {
			return nil
		}
		if err := sleepContext(ctx, wait); err != nil {
			return err
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *ResourceLimiter) markStarted() {
	l.mu.Lock()
	l.lastStart = time.Now()
	l.mu.Unlock()
}

func (l *ResourceLimiter) canStart() bool {
	if ForegroundActive() {
		return false
	}
	if l.policy.LoadTarget > 0 {
		if load, ok := readLoadAvg1(); ok && load > l.policy.LoadTarget {
			return false
		}
	}
	if l.policy.MinFreeMemoryBytes > 0 {
		if available, ok := readMemAvailable(); ok && available < l.policy.MinFreeMemoryBytes {
			return false
		}
	}
	return true
}

func readLoadAvg1() (float64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	return value, err == nil
}

func readMemAvailable() (uint64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

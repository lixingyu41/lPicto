package api

import (
	"context"
	"sync"
)

type mediaIOPriority int

const (
	mediaIOPriorityPreload  mediaIOPriority = 20
	mediaIOPriorityAhead    mediaIOPriority = 80
	mediaIOPriorityFullWarm mediaIOPriority = 90
	mediaIOPriorityCurrent  mediaIOPriority = 100
)

type mediaIOWaiter struct {
	ready    chan struct{}
	priority mediaIOPriority
	order    uint64
	granted  bool
}

type mediaIOScheduler struct {
	mu       sync.Mutex
	active   bool
	sequence uint64
	waiters  []*mediaIOWaiter
}

func (s *mediaIOScheduler) acquire(ctx context.Context, priority mediaIOPriority) (func(), error) {
	s.mu.Lock()
	if !s.active {
		s.active = true
		s.mu.Unlock()
		return s.release, nil
	}
	s.sequence++
	waiter := &mediaIOWaiter{ready: make(chan struct{}), priority: priority, order: s.sequence}
	s.waiters = append(s.waiters, waiter)
	s.mu.Unlock()

	select {
	case <-waiter.ready:
		return s.release, nil
	case <-ctx.Done():
		s.mu.Lock()
		if waiter.granted {
			s.mu.Unlock()
			return s.release, nil
		}
		for index, queued := range s.waiters {
			if queued == waiter {
				s.waiters = append(s.waiters[:index], s.waiters[index+1:]...)
				break
			}
		}
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *mediaIOScheduler) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.waiters) == 0 {
		s.active = false
		return
	}
	selected := 0
	for index := 1; index < len(s.waiters); index++ {
		candidate := s.waiters[index]
		current := s.waiters[selected]
		if candidate.priority > current.priority || (candidate.priority == current.priority && candidate.order < current.order) {
			selected = index
		}
	}
	waiter := s.waiters[selected]
	s.waiters = append(s.waiters[:selected], s.waiters[selected+1:]...)
	waiter.granted = true
	close(waiter.ready)
}

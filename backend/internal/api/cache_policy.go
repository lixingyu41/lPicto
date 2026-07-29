package api

import (
	"context"
	"time"
)

func (s *Server) startUnifiedCacheSweeper() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := s.cachePolicy.EnsureCapacity(ctx, 0)
			cancel()
			if err != nil && s.logger != nil {
				s.logger.Warn("enforce unified cache capacity failed", "error", err)
			}
			s.cacheMu.Lock()
			s.cacheStatsAt = time.Time{}
			s.cacheMu.Unlock()
		}
	}()
}

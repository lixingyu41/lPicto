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
			if _, cleanupErr := s.cachePolicy.CleanupAbandoned(ctx, time.Minute); cleanupErr != nil && s.logger != nil {
				s.logger.Warn("cleanup abandoned cache outputs failed", "error", cleanupErr)
			}
			if s.aiStager != nil {
				if cleanupErr := s.aiStager.CleanupAbandoned(ctx); cleanupErr != nil && s.logger != nil {
					s.logger.Warn("cleanup abandoned AI staging failed", "error", cleanupErr)
				}
			}
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

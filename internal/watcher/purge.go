package watcher

import (
	"context"
	"time"
)

func (s *Supervisor) PurgeLoop(ctx context.Context) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.store.PurgeOldSeen(30 * 24 * time.Hour); err != nil {
				s.log.Warn("purge failed", "err", err.Error())
			}
		}
	}
}

package watcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fentezi/olx-scraper/internal/notifier"
	"github.com/fentezi/olx-scraper/internal/scraper"
	"github.com/fentezi/olx-scraper/internal/storage"
)

const (
	maxPagesPerPoll    = 5
	notifyFailureCount = 5
	pauseFailureCount  = 50
	maxRateLimitWait   = 10 * time.Minute
	maxBackoff         = 5 * time.Minute
)

// LangOf resolves the UI language for a user. Provided by the caller so the
// watcher does not depend on the Telegram-layer language cache.
type LangOf func(userID int64) string

type Supervisor struct {
	store    *storage.Store
	scraper  *scraper.Scraper
	notifier *notifier.Notifier
	log      *slog.Logger
	langOf   LangOf

	mu      sync.Mutex
	cancels map[int64]map[string]context.CancelFunc
}

func NewSupervisor(
	store *storage.Store,
	scr *scraper.Scraper,
	notif *notifier.Notifier,
	log *slog.Logger,
	langOf LangOf,
) *Supervisor {
	return &Supervisor{
		store:    store,
		scraper:  scr,
		notifier: notif,
		log:      log,
		langOf:   langOf,
		cancels:  make(map[int64]map[string]context.CancelFunc),
	}
}

func (s *Supervisor) Spawn(parentCtx context.Context, userID int64, listURL string) {
	s.mu.Lock()
	if s.cancels[userID] == nil {
		s.cancels[userID] = make(map[string]context.CancelFunc)
	}
	if existing, ok := s.cancels[userID][listURL]; ok {
		existing()
	}
	watchCtx, watchCancel := context.WithCancel(parentCtx)
	s.cancels[userID][listURL] = watchCancel
	s.mu.Unlock()
	go s.parseLoop(watchCtx, userID, listURL)
}

func (s *Supervisor) Cancel(userID int64, listURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inner := s.cancels[userID]
	if inner == nil {
		return
	}
	if c, ok := inner[listURL]; ok {
		c()
		delete(inner, listURL)
	}
	if len(inner) == 0 {
		delete(s.cancels, userID)
	}
}

func (s *Supervisor) HasRunning(userID int64, listURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inner, ok := s.cancels[userID]; ok {
		_, running := inner[listURL]
		return running
	}
	return false
}

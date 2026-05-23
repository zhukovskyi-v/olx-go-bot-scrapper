package watcher

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
	"github.com/fentezi/olx-scraper/internal/scraper"
)

func (s *Supervisor) parseLoop(ctx context.Context, id int64, listURL string) {
	s.log.Info("parseLoop started", "id", id, "url", listURL)
	defer s.log.Info("parseLoop exited", "id", id, "url", listURL)

	consecutiveFailures := 0
	for {
		w, err := s.store.GetWatchByURL(id, listURL)
		if err != nil {
			s.log.Warn("getWatchByURL failed", "id", id, "err", err.Error())
			if !sleepCtx(ctx, 30*time.Second) {
				return
			}
			continue
		}

		pollErr := s.pollOnce(ctx, w)
		if pollErr != nil {
			consecutiveFailures++
			s.log.Warn("poll failed", "id", id, "attempt", consecutiveFailures, "err", pollErr.Error())

			if consecutiveFailures == notifyFailureCount {
				s.notifier.SendText(ctx, id, i18n.T(s.langOf(id), "notify.olx_unavailable"))
			}
			if consecutiveFailures >= pauseFailureCount {
				if err := s.store.SetPaused(id, listURL, true); err != nil {
					s.log.Warn("setPaused failed", "err", err.Error())
				}
				s.notifier.SendText(ctx, id, i18n.T(s.langOf(id), "notify.paused_after_failures", w.LocalID, w.LocalID))
				s.Cancel(id, listURL)
				return
			}

			var rl *scraper.RateLimitError
			var wait time.Duration
			if errors.As(pollErr, &rl) {
				wait = rl.RetryAfter
				if wait > maxRateLimitWait {
					wait = maxRateLimitWait
				}
			} else {
				wait = time.Duration(math.Min(maxBackoff.Seconds(), 5*math.Pow(2, float64(consecutiveFailures-1)))) * time.Second
			}
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}

		consecutiveFailures = 0
		if !w.Bootstrapped {
			if err := s.store.SetBootstrapped(id, listURL); err != nil {
				s.log.Warn("setBootstrapped failed", "err", err.Error())
			}
		}

		sleepDuration := time.Duration(30+rand.Intn(90)) * time.Second
		if !sleepCtx(ctx, sleepDuration) {
			return
		}
	}
}

func (s *Supervisor) pollOnce(ctx context.Context, w domain.Watch) error {
	maxPages := maxPagesPerPoll
	if !w.Bootstrapped {
		maxPages = 1
	}

	f := domain.Filter{
		PriceMin: w.PriceMin,
		PriceMax: w.PriceMax,
		Include:  w.IncludeKw,
		Exclude:  w.ExcludeKw,
	}

	for page := 1; page <= maxPages; page++ {
		pageURL, err := scraper.PaginatedURL(w.URL, page)
		if err != nil {
			return err
		}

		ads, err := s.scraper.FetchList(pageURL)
		if err != nil {
			return err
		}

		sawSeen := false
		for _, ad := range ads {
			if ad.ID == "" {
				continue
			}
			seen, err := s.store.HasSeen(w.UserID, ad.ID)
			if err != nil {
				s.log.Warn("hasSeen failed", "err", err.Error())
				continue
			}
			if seen {
				sawSeen = true
				continue
			}

			if !w.Bootstrapped {
				if err := s.store.MarkSeen(w.UserID, ad.ID); err != nil {
					s.log.Warn("markSeen failed", "err", err.Error())
				}
				continue
			}

			detail, derr := s.scraper.FetchDetail(ad.URL)
			if derr != nil {
				s.log.Warn("detail fetch failed, sending list info", "id", w.UserID, "url", ad.URL, "err", derr.Error())
			} else {
				if detail.Description != "" {
					ad.Description = detail.Description
				}
				if detail.Price != "" {
					ad.Price = detail.Price
				}
				if detail.District != "" {
					ad.District = detail.District
				}
				if detail.Image != "" {
					ad.Image = detail.Image
				}
				if detail.Title != "" {
					ad.Title = detail.Title
				}
			}

			if err := s.store.MarkSeen(w.UserID, ad.ID); err != nil {
				s.log.Warn("markSeen failed", "err", err.Error())
			}

			if accept, reason := f.Accept(ad); !accept {
				s.log.Debug("ad filtered", "user", w.UserID, "ad", ad.ID, "reason", reason)
				continue
			}

			if err := s.notifier.SendAd(ctx, w.UserID, ad, s.langOf(w.UserID)); err != nil {
				s.log.Warn("send failed", "id", w.UserID, "err", err.Error())
			} else {
				s.log.Info("sent ad", "id", w.UserID, "adID", ad.ID, "title", ad.Title)
			}
		}

		if sawSeen {
			break
		}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

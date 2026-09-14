package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/fentezi/olx-scraper/internal/config"
	"github.com/fentezi/olx-scraper/internal/logger"
	"github.com/fentezi/olx-scraper/internal/notifier"
	"github.com/fentezi/olx-scraper/internal/scraper"
	"github.com/fentezi/olx-scraper/internal/storage"
	"github.com/fentezi/olx-scraper/internal/telegram"
	"github.com/fentezi/olx-scraper/internal/watcher"
	tele "gopkg.in/telebot.v3"
)

func main() {
	cfg, err := config.Load()
	log := logger.New(cfgEnv(cfg))
	if err != nil {
		log.Error("config load failed", logger.Err(err))
		os.Exit(1)
	}
	log.Info("application started")

	store, err := storage.Open(cfg.DBURL)
	if err != nil {
		log.Error("failed to open store", logger.Err(err))
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// telegram.LoggingPoller instead of tele.LongPoller: the stock poller
	// silently discards every getUpdates error unless Verbose dumps all API
	// traffic, which hides a 409 conflict — the failure that makes the bot
	// receive nothing while looking perfectly healthy.
	tb, err := tele.NewBot(tele.Settings{
		Token:   cfg.Token,
		Poller:  &telegram.LoggingPoller{Timeout: 10 * time.Second, Log: log},
		Verbose: false,
	})
	if err != nil {
		log.Error("failed to init bot", logger.Err(err))
		os.Exit(1)
	}

	scr, err := scraper.New()
	if err != nil {
		log.Error("failed to init scraper", logger.Err(err))
		os.Exit(1)
	}
	notif := notifier.New(tb, log)

	// Bot needs Supervisor for handler-driven Spawn/Cancel; Supervisor needs
	// Bot.UserLang for poll-loop messages. Build Bot first, then Supervisor
	// referencing bot.UserLang, then wire it back into Bot.
	bot := telegram.NewBot(tb, store, notif, log)
	super := watcher.NewSupervisor(store, scr, notif, log, bot.UserLang)
	bot.AttachSupervisor(super)
	bot.Register(ctx)

	watches, err := store.AllWatches()
	if err != nil {
		log.Error("failed to load watches", logger.Err(err))
	} else {
		for uid, urls := range watches {
			for _, u := range urls {
				super.Spawn(ctx, uid, u)
			}
		}
	}

	go super.PurgeLoop(ctx)
	go tb.Start()
	log.Info("bot started", "goroutines", runtime.NumGoroutine())

	<-ctx.Done()
	log.Info("shutting down")
	tb.Stop()
}

func cfgEnv(c *config.Config) string {
	if c == nil {
		return ""
	}
	return c.Env
}

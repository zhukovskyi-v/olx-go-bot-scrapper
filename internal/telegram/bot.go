package telegram

import (
	"context"
	"log/slog"
	"sync"

	"github.com/fentezi/olx-scraper/internal/notifier"
	"github.com/fentezi/olx-scraper/internal/storage"
	"github.com/fentezi/olx-scraper/internal/watcher"
	tele "gopkg.in/telebot.v3"
)

const maxWatchNameLength = 64

type Bot struct {
	tb       *tele.Bot
	store    *storage.Store
	super    *watcher.Supervisor
	notifier *notifier.Notifier
	log      *slog.Logger

	pendingMu sync.Mutex
	pending   map[int64]pendingInput

	langCache       sync.Map // map[int64]string
	menuLabelAction map[string]string

	ctx context.Context
}

// NewBot builds the Bot with all collaborators except the watcher Supervisor.
// Call AttachSupervisor before Register — handlers that spawn/cancel watches
// dereference it.
func NewBot(
	tb *tele.Bot,
	store *storage.Store,
	notif *notifier.Notifier,
	log *slog.Logger,
) *Bot {
	b := &Bot{
		tb:              tb,
		store:           store,
		notifier:        notif,
		log:             log,
		pending:         make(map[int64]pendingInput),
		menuLabelAction: make(map[string]string),
	}
	b.initMenuLabels()
	return b
}

// AttachSupervisor wires the watcher.Supervisor after both Bot and Supervisor
// are constructed (Supervisor needs Bot.UserLang at construction).
func (b *Bot) AttachSupervisor(s *watcher.Supervisor) {
	b.super = s
}

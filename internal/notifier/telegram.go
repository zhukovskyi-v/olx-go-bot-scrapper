package notifier

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
	"golang.org/x/time/rate"
	tele "gopkg.in/telebot.v3"
)

type Notifier struct {
	bot    *tele.Bot
	global *rate.Limiter
	chats  *chatLimiters
	log    *slog.Logger
}

func New(bot *tele.Bot, log *slog.Logger) *Notifier {
	return &Notifier{
		bot:    bot,
		global: newGlobalLimiter(),
		chats:  newChatLimiters(),
		log:    log,
	}
}

// ChatLimiter exposes the per-chat limiter (used by tests).
func (n *Notifier) ChatLimiter(id int64) *rate.Limiter {
	return n.chats.get(id)
}

func (n *Notifier) waitSlots(ctx context.Context, chatID int64) error {
	if err := n.global.Wait(ctx); err != nil {
		return err
	}
	return n.chats.get(chatID).Wait(ctx)
}

func (n *Notifier) SendText(ctx context.Context, userID int64, msg string) {
	if err := n.waitSlots(ctx, userID); err != nil {
		return
	}
	if _, err := n.bot.Send(tele.ChatID(userID), msg); err != nil {
		n.log.Warn("notify failed", "id", userID, "err", err.Error())
	}
}

func (n *Notifier) SendAd(ctx context.Context, userID int64, ad domain.Ad, lang string) error {
	caption := ad.Title
	if ad.Price != "" {
		caption += "\n" + i18n.T(lang, "ad.caption_price", ad.Price)
	}
	location := ad.City
	if ad.District != "" {
		if location != "" {
			location += ", " + ad.District
		} else {
			location = ad.District
		}
	}
	if location != "" {
		caption += "\n" + i18n.T(lang, "ad.caption_location", location)
	}

	btnURL := tele.InlineButton{
		Unique: "myButton",
		Text:   i18n.T(lang, "ad.button_label"),
		URL:    ad.URL,
	}
	markup := &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btnURL}}}

	send := func(payload interface{}) error {
		if err := n.waitSlots(ctx, userID); err != nil {
			return err
		}
		_, err := n.bot.Send(tele.ChatID(userID), payload, &tele.SendOptions{ReplyMarkup: markup})
		return err
	}

	sendWithRetry := func(payload interface{}) error {
		err := send(payload)
		if err == nil {
			return nil
		}
		var fe tele.FloodError
		if errors.As(err, &fe) {
			wait := time.Duration(fe.RetryAfter) * time.Second
			if wait <= 0 {
				wait = 5 * time.Second
			}
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
			return send(payload)
		}
		return err
	}

	if ad.Image != "" {
		photo := &tele.Photo{File: tele.FromURL(ad.Image), Caption: caption}
		if err := sendWithRetry(photo); err == nil {
			return nil
		}
	}
	return sendWithRetry(caption)
}

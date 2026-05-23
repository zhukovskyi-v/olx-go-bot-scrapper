package notifier

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
	"golang.org/x/time/rate"
	tele "gopkg.in/telebot.v3"
)

const (
	maxAlbumPhotos = 10
	captionBudget  = 1000 // Telegram caption ~1024; leave margin.
	textBudget     = 4000 // Telegram text ~4096; leave margin.

	CbShowFullDesc = "show_full_desc"
)

type Notifier struct {
	bot    *tele.Bot
	global *rate.Limiter
	chats  *chatLimiters
	desc   *descCache
	log    *slog.Logger
}

func New(bot *tele.Bot, log *slog.Logger) *Notifier {
	return &Notifier{
		bot:    bot,
		global: newGlobalLimiter(),
		chats:  newChatLimiters(),
		desc:   newDescCache(),
		log:    log,
	}
}

// ChatLimiter exposes the per-chat limiter (used by tests).
func (n *Notifier) ChatLimiter(id int64) *rate.Limiter {
	return n.chats.get(id)
}

// GetDescription returns a cached full description for ad ID, if available.
func (n *Notifier) GetDescription(adID string) (string, bool) {
	return n.desc.Get(adID)
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

// doWithRetry runs action under the per-chat + global limiter and retries once
// on tele.FloodError honoring RetryAfter.
func (n *Notifier) doWithRetry(ctx context.Context, userID int64, action func() error) error {
	run := func() error {
		if err := n.waitSlots(ctx, userID); err != nil {
			return err
		}
		return action()
	}
	err := run()
	if err == nil {
		return nil
	}
	var fe tele.FloodError
	if !errors.As(err, &fe) {
		return err
	}
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
	return run()
}

func (n *Notifier) SendAd(ctx context.Context, userID int64, ad domain.Ad, lang string) error {
	to := tele.ChatID(userID)
	nImages := len(ad.Images)

	if nImages >= 2 {
		// Album path: Telegram disallows inline keyboards on media groups, so
		// embed the URL inline in the caption (Telegram auto-detects URLs).
		// Only spawn a follow-up msg when the description was truncated, to
		// surface a "Read full description" button.
		urlLine := "\n\n" + i18n.T(lang, "ad.caption_open_url", ad.URL)
		budget := captionBudget - runeLen(urlLine)
		caption, truncated := buildCaption(ad, lang, budget)
		caption += urlLine

		if truncated {
			n.desc.Put(ad.ID, ad.Description)
		}

		if err := n.sendAlbum(ctx, userID, ad.Images, caption); err != nil {
			n.log.Warn("send album failed, fallback single photo",
				"id", userID, "err", err.Error())
			_ = n.doWithRetry(ctx, userID, func() error {
				photo := &tele.Photo{File: tele.FromURL(ad.Images[0]), Caption: caption}
				_, err := n.bot.Send(to, photo)
				return err
			})
		}
		if !truncated {
			return nil
		}
		return n.doWithRetry(ctx, userID, func() error {
			_, err := n.bot.Send(to, i18n.T(lang, "ad.tap_full_desc"), &tele.SendOptions{
				ReplyMarkup:           buildFullDescMarkup(ad, lang),
				DisableWebPagePreview: true,
			})
			return err
		})
	}

	// 0 or 1 image: buttons attach to the main msg directly.
	budget := captionBudget
	if nImages == 0 {
		budget = textBudget
	}
	caption, truncated := buildCaption(ad, lang, budget)
	if truncated {
		n.desc.Put(ad.ID, ad.Description)
	}
	markup := buildMarkup(ad, lang, truncated)

	if nImages == 0 {
		return n.doWithRetry(ctx, userID, func() error {
			_, err := n.bot.Send(to, caption, &tele.SendOptions{
				ReplyMarkup:           markup,
				DisableWebPagePreview: true,
			})
			return err
		})
	}
	return n.doWithRetry(ctx, userID, func() error {
		photo := &tele.Photo{File: tele.FromURL(ad.Images[0]), Caption: caption}
		_, err := n.bot.Send(to, photo, &tele.SendOptions{ReplyMarkup: markup})
		return err
	})
}

func (n *Notifier) sendAlbum(ctx context.Context, userID int64, images []string, caption string) error {
	to := tele.ChatID(userID)
	limit := len(images)
	if limit > maxAlbumPhotos {
		limit = maxAlbumPhotos
	}
	album := make(tele.Album, 0, limit)
	for i := 0; i < limit; i++ {
		p := &tele.Photo{File: tele.FromURL(images[i])}
		if i == 0 {
			p.Caption = caption
		}
		album = append(album, p)
	}
	return n.doWithRetry(ctx, userID, func() error {
		_, err := n.bot.SendAlbum(to, album)
		return err
	})
}

func buildMarkup(ad domain.Ad, lang string, truncated bool) *tele.ReplyMarkup {
	btnOpen := tele.InlineButton{
		Unique: "myButton",
		Text:   i18n.T(lang, "ad.button_label"),
		URL:    ad.URL,
	}
	row := []tele.InlineButton{btnOpen}
	if truncated && ad.ID != "" {
		row = append(row, tele.InlineButton{
			Unique: CbShowFullDesc,
			Text:   i18n.T(lang, "ad.button_full_desc"),
			Data:   ad.ID,
		})
	}
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{row}}
}

// buildFullDescMarkup is used on album follow-ups: only the "Read full
// description" button (the URL already lives in the album caption).
func buildFullDescMarkup(ad domain.Ad, lang string) *tele.ReplyMarkup {
	btn := tele.InlineButton{
		Unique: CbShowFullDesc,
		Text:   i18n.T(lang, "ad.button_full_desc"),
		Data:   ad.ID,
	}
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{btn}}}
}

// buildCaption assembles title + price + location + (truncated) description so
// the total fits within budget runes. Returns (caption, descriptionTruncated).
// descriptionTruncated is true only when the description was cut; an empty
// description always reports false.
func buildCaption(ad domain.Ad, lang string, budget int) (string, bool) {
	base := buildBase(ad, lang)
	if ad.Description == "" {
		return base, false
	}
	available := budget - runeLen(base) - 2 // for "\n\n"
	if available <= 0 {
		return base, true
	}
	descPart, didTrunc := truncateBySentence(ad.Description, available)
	if descPart == "" {
		return base, didTrunc
	}
	return base + "\n\n" + descPart, didTrunc
}

func buildBase(ad domain.Ad, lang string) string {
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
	return caption
}

// truncateBySentence trims s to fit within maxRunes runes, preferring sentence
// boundaries (.!?\n). Falls back to word boundary, then to a hard cut. If
// truncated, the result ends with "…".
func truncateBySentence(s string, maxRunes int) (string, bool) {
	if maxRunes <= 0 {
		return "", s != ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s, false
	}
	cut := maxRunes - 1 // reserve 1 rune for "…"
	if cut <= 0 {
		return "…", true
	}
	head := r[:cut]
	floor := cut * 7 / 10 // do not search before the last 30% of the window

	for i := len(head) - 1; i >= floor; i-- {
		c := head[i]
		if c == '.' || c == '!' || c == '?' || c == '\n' {
			out := strings.TrimRight(string(head[:i+1]), " \t\n")
			return out + "…", true
		}
	}
	for i := len(head) - 1; i >= floor; i-- {
		c := head[i]
		if c == ' ' || c == '\n' || c == '\t' {
			return strings.TrimRight(string(head[:i]), " \t\n") + "…", true
		}
	}
	return string(head) + "…", true
}

func runeLen(s string) int {
	return len([]rune(s))
}

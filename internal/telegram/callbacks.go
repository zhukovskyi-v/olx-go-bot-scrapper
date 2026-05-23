package telegram

import (
	"strconv"
	"strings"

	"github.com/fentezi/olx-scraper/internal/i18n"
	tele "gopkg.in/telebot.v3"
)

// Inline button uniques (callback routing keys).
const (
	cbWatchPause   = "watch_pause"
	cbWatchResume  = "watch_resume"
	cbWatchRemove  = "watch_remove"
	cbWatchFilter  = "watch_filter"
	cbFilterPrice  = "filter_price"
	cbFilterInc    = "filter_include"
	cbFilterExc    = "filter_exclude"
	cbFilterClear  = "filter_clear"
	cbFilterCancel = "filter_cancel"
	cbLangSet      = "lang_set"
)

func (b *Bot) registerCallbacks() {
	b.tb.Handle(&tele.InlineButton{Unique: cbWatchPause}, b.cbWatchPause)
	b.tb.Handle(&tele.InlineButton{Unique: cbWatchResume}, b.cbWatchResume)
	b.tb.Handle(&tele.InlineButton{Unique: cbWatchRemove}, b.cbWatchRemove)
	b.tb.Handle(&tele.InlineButton{Unique: cbWatchFilter}, b.cbWatchFilter)
	b.tb.Handle(&tele.InlineButton{Unique: cbFilterPrice}, b.cbFilterField(pendingFilterPrice, "wiz.prompt_price"))
	b.tb.Handle(&tele.InlineButton{Unique: cbFilterInc}, b.cbFilterField(pendingFilterInclude, "wiz.prompt_include"))
	b.tb.Handle(&tele.InlineButton{Unique: cbFilterExc}, b.cbFilterField(pendingFilterExclude, "wiz.prompt_exclude"))
	b.tb.Handle(&tele.InlineButton{Unique: cbFilterClear}, b.cbFilterClear)
	b.tb.Handle(&tele.InlineButton{Unique: cbFilterCancel}, b.cbFilterCancel)
	b.tb.Handle(&tele.InlineButton{Unique: cbLangSet}, b.cbLangSet)
}

func parseCBLocalID(c tele.Context) (int, bool) {
	data := strings.TrimSpace(c.Data())
	id, err := strconv.Atoi(data)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (b *Bot) cbWatchPause(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	if err := c.Respond(); err != nil {
		b.log.Debug("respond failed", "err", err.Error())
	}
	localID, ok := parseCBLocalID(c)
	if !ok {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	pausedURL, err := b.store.SetPausedByLocalID(userID, localID, true)
	if err != nil {
		b.log.Warn("pause cb failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if pausedURL == "" {
		return c.Send(i18n.T(lang, "pause.not_found", localID))
	}
	b.super.Cancel(userID, pausedURL)
	if w, err := b.store.GetWatchByLocalID(userID, localID); err == nil {
		_ = c.Edit(formatWatchCard(lang, w), rowKeyboard(w, lang))
	}
	return c.Send(i18n.T(lang, "pause.one_success", localID))
}

func (b *Bot) cbWatchResume(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	_ = c.Respond()
	localID, ok := parseCBLocalID(c)
	if !ok {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	resumedURL, err := b.store.ResumeByLocalID(userID, localID)
	if err != nil {
		b.log.Warn("resume cb failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if resumedURL == "" {
		return c.Send(i18n.T(lang, "resume.not_found", localID))
	}
	if !b.super.HasRunning(userID, resumedURL) {
		b.super.Spawn(b.ctx, userID, resumedURL)
	}
	if w, err := b.store.GetWatchByLocalID(userID, localID); err == nil {
		_ = c.Edit(formatWatchCard(lang, w), rowKeyboard(w, lang))
	}
	return c.Send(i18n.T(lang, "resume.one_success", localID))
}

func (b *Bot) cbWatchRemove(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	_ = c.Respond()
	localID, ok := parseCBLocalID(c)
	if !ok {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	removedURL, err := b.store.RemoveWatchByLocalID(userID, localID)
	if err != nil {
		b.log.Warn("remove cb failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if removedURL == "" {
		return c.Send(i18n.T(lang, "remove.not_found", localID))
	}
	b.super.Cancel(userID, removedURL)
	_ = c.Delete()
	return c.Send(i18n.T(lang, "remove.success", localID))
}

func (b *Bot) cbWatchFilter(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	_ = c.Respond()
	localID, ok := parseCBLocalID(c)
	if !ok {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	return c.Send(i18n.T(lang, "wiz.choose", localID), filterWizardKeyboard(localID, lang))
}

func (b *Bot) cbFilterField(kind pendingKind, promptKey string) tele.HandlerFunc {
	return func(c tele.Context) error {
		userID := c.Sender().ID
		lang := b.UserLang(userID)
		_ = c.Respond()
		localID, ok := parseCBLocalID(c)
		if !ok {
			return c.Send(i18n.T(lang, "filter.bad_id"))
		}
		b.setPending(userID, pendingInput{kind: kind, localID: localID})
		_ = c.Delete()
		return c.Send(i18n.T(lang, promptKey, localID))
	}
}

func (b *Bot) cbFilterClear(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	_ = c.Respond()
	localID, ok := parseCBLocalID(c)
	if !ok {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	updated, err := b.store.ClearFilters(userID, localID)
	if err != nil {
		b.log.Warn("clearFilters cb failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if !updated {
		return c.Send(i18n.T(lang, "filter.target_not_found", localID))
	}
	_ = c.Delete()
	return c.Send(i18n.T(lang, "filter.cleared", localID))
}

func (b *Bot) cbFilterCancel(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	_ = c.Respond()
	_ = c.Delete()
	return c.Send(i18n.T(lang, "wiz.cancelled"))
}

func (b *Bot) cbLangSet(c tele.Context) error {
	userID := c.Sender().ID
	_ = c.Respond()
	code := strings.ToLower(strings.TrimSpace(c.Data()))
	if !i18n.IsSupported(code) {
		return c.Send(i18n.T(b.UserLang(userID), "lang.unsupported", code, i18n.SupportedList()))
	}
	_ = c.Delete()
	return b.applyLang(c, userID, code)
}

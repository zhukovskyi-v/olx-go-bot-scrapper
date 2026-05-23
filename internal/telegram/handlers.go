package telegram

import (
	"context"
	"strconv"
	"strings"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
	"github.com/fentezi/olx-scraper/internal/logger"
	tele "gopkg.in/telebot.v3"
)

// Register binds all Telegram command, text, and callback handlers. ctx is the
// root context; spawned watches inherit it so SIGTERM cleanly cancels them.
func (b *Bot) Register(ctx context.Context) {
	b.ctx = ctx

	if err := b.tb.SetCommands([]tele.Command{
		{Text: "start", Description: "Start / open menu"},
		{Text: "menu", Description: "Show main menu"},
		{Text: "addurl", Description: "Add an OLX URL to track"},
		{Text: "list", Description: "List your watches"},
		{Text: "remove", Description: "Remove a watch by id"},
		{Text: "pause", Description: "Pause a watch (or all)"},
		{Text: "resume", Description: "Resume a watch (or all)"},
		{Text: "filter", Description: "Configure filters for a watch"},
		{Text: "name", Description: "Rename a watch"},
		{Text: "lang", Description: "Switch UI language"},
		{Text: "help", Description: "Show help"},
	}); err != nil {
		b.log.Warn("setCommands failed", "err", err.Error())
	}

	b.tb.Handle("/start", b.handleStart)
	b.tb.Handle("/addurl", b.handleAddURL)
	b.tb.Handle("/menu", b.handleMenu)
	b.tb.Handle(tele.OnText, b.handleText)
	b.tb.Handle("/list", b.handleList)
	b.tb.Handle("/remove", b.handleRemove)
	b.tb.Handle("/pause", b.handlePause)
	b.tb.Handle("/resume", b.handleResume)
	b.tb.Handle("/filter", b.handleFilter)
	b.tb.Handle("/name", b.handleName)
	b.tb.Handle("/lang", b.handleLang)
	b.tb.Handle("/help", b.handleHelp)

	b.registerCallbacks()
}

func (b *Bot) handleStart(c tele.Context) error {
	userID := c.Sender().ID
	detected := i18n.NormalizeLang(c.Sender().LanguageCode)
	if err := b.store.EnsureUser(userID, detected); err != nil {
		b.log.Warn("ensureUser failed", "err", err.Error())
	}
	lang := b.UserLang(userID)
	return b.sendWithMenu(c, lang, i18n.T(lang, "start.greeting", c.Sender().Username))
}

func (b *Bot) handleAddURL(c tele.Context) error {
	userID := c.Sender().ID
	b.setPending(userID, pendingInput{kind: pendingAddURL})
	return c.Send(i18n.T(b.UserLang(userID), "addurl.prompt"))
}

func (b *Bot) handleMenu(c tele.Context) error {
	lang := b.UserLang(c.Sender().ID)
	return b.sendWithMenu(c, lang, i18n.T(lang, "menu.shown"))
}

func (b *Bot) handleList(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	watches, err := b.store.ListWatchesDetailed(userID)
	if err != nil {
		b.log.Warn("list failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if len(watches) == 0 {
		return c.Send(i18n.T(lang, "list.empty"))
	}
	if err := c.Send(i18n.T(lang, "list.header")); err != nil {
		return err
	}
	for _, w := range watches {
		text := formatWatchCard(lang, w)
		markup := rowKeyboard(w, lang)
		if err := c.Send(text, markup); err != nil {
			b.log.Warn("list send failed", "err", err.Error())
		}
	}
	return nil
}

func (b *Bot) handleRemove(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	args := c.Args()
	if len(args) != 1 {
		return c.Send(i18n.T(lang, "remove.usage"))
	}
	localID, err := strconv.Atoi(args[0])
	if err != nil || localID <= 0 {
		return c.Send(i18n.T(lang, "remove.usage"))
	}
	removedURL, err := b.store.RemoveWatchByLocalID(userID, localID)
	if err != nil {
		b.log.Warn("remove failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if removedURL == "" {
		return c.Send(i18n.T(lang, "remove.not_found", localID))
	}
	b.super.Cancel(userID, removedURL)
	return c.Send(i18n.T(lang, "remove.success", localID))
}

func (b *Bot) handlePause(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	args := c.Args()
	if len(args) == 0 {
		urls, err := b.store.PauseAllByUser(userID)
		if err != nil {
			b.log.Warn("pauseAll failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if len(urls) == 0 {
			return c.Send(i18n.T(lang, "pause.none_active"))
		}
		for _, u := range urls {
			b.super.Cancel(userID, u)
		}
		return c.Send(i18n.T(lang, "pause.all_success", len(urls)))
	}
	localID, err := strconv.Atoi(args[0])
	if err != nil || localID <= 0 {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	pausedURL, err := b.store.SetPausedByLocalID(userID, localID, true)
	if err != nil {
		b.log.Warn("pause failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if pausedURL == "" {
		return c.Send(i18n.T(lang, "pause.not_found", localID))
	}
	b.super.Cancel(userID, pausedURL)
	return c.Send(i18n.T(lang, "pause.one_success", localID))
}

func (b *Bot) handleResume(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	args := c.Args()
	if len(args) == 0 {
		urls, err := b.store.ResumeAllByUser(userID)
		if err != nil {
			b.log.Warn("resumeAll failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if len(urls) == 0 {
			return c.Send(i18n.T(lang, "resume.none_paused"))
		}
		for _, u := range urls {
			if !b.super.HasRunning(userID, u) {
				b.super.Spawn(b.ctx, userID, u)
			}
		}
		return c.Send(i18n.T(lang, "resume.all_success", len(urls)))
	}
	localID, err := strconv.Atoi(args[0])
	if err != nil || localID <= 0 {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	resumedURL, err := b.store.ResumeByLocalID(userID, localID)
	if err != nil {
		b.log.Warn("resume failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if resumedURL == "" {
		return c.Send(i18n.T(lang, "resume.not_found", localID))
	}
	if !b.super.HasRunning(userID, resumedURL) {
		b.super.Spawn(b.ctx, userID, resumedURL)
	}
	return c.Send(i18n.T(lang, "resume.one_success", localID))
}

func (b *Bot) handleFilter(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	args := c.Args()
	if len(args) < 2 {
		return c.Send(i18n.T(lang, "filter.usage"))
	}
	localID, err := strconv.Atoi(args[0])
	if err != nil || localID <= 0 {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	field := strings.ToLower(args[1])
	rest := strings.TrimSpace(strings.Join(args[2:], " "))

	switch field {
	case "price":
		lo, hi, ok := domain.ParsePriceRange(rest)
		if !ok {
			return c.Send(i18n.T(lang, "filter.bad_price"))
		}
		updated, err := b.store.SetPriceFilter(userID, localID, lo, hi)
		if err != nil {
			b.log.Warn("setPriceFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", localID))
		}
		return c.Send(i18n.T(lang, "filter.price_set", localID, formatBound(lo), formatBound(hi)))
	case "include":
		if rest == "" {
			return c.Send(i18n.T(lang, "filter.usage"))
		}
		words := domain.NormalizeKeywords(strings.Split(rest, ","))
		updated, err := b.store.SetIncludeFilter(userID, localID, words)
		if err != nil {
			b.log.Warn("setIncludeFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", localID))
		}
		return c.Send(i18n.T(lang, "filter.include_set", localID, strings.Join(words, ", ")))
	case "exclude":
		if rest == "" {
			return c.Send(i18n.T(lang, "filter.usage"))
		}
		words := domain.NormalizeKeywords(strings.Split(rest, ","))
		updated, err := b.store.SetExcludeFilter(userID, localID, words)
		if err != nil {
			b.log.Warn("setExcludeFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", localID))
		}
		return c.Send(i18n.T(lang, "filter.exclude_set", localID, strings.Join(words, ", ")))
	case "clear":
		updated, err := b.store.ClearFilters(userID, localID)
		if err != nil {
			b.log.Warn("clearFilters failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", localID))
		}
		return c.Send(i18n.T(lang, "filter.cleared", localID))
	default:
		return c.Send(i18n.T(lang, "filter.bad_field", field))
	}
}

func (b *Bot) handleName(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	args := c.Args()
	if len(args) < 2 {
		return c.Send(i18n.T(lang, "name.usage"))
	}
	localID, err := strconv.Atoi(args[0])
	if err != nil || localID <= 0 {
		return c.Send(i18n.T(lang, "filter.bad_id"))
	}
	name := strings.Join(args[1:], " ")
	if len(name) > maxWatchNameLength {
		name = name[:maxWatchNameLength]
	}
	updated, err := b.store.SetWatchName(userID, localID, name)
	if err != nil {
		b.log.Warn("setWatchName failed", "err", err.Error())
		return c.Send(i18n.T(lang, "addurl.save_failed"))
	}
	if !updated {
		return c.Send(i18n.T(lang, "name.not_found", localID))
	}
	return c.Send(i18n.T(lang, "name.set", localID, name))
}

func (b *Bot) handleLang(c tele.Context) error {
	userID := c.Sender().ID
	args := c.Args()
	current := b.UserLang(userID)
	if len(args) == 0 {
		return c.Send(i18n.T(current, "menu.lang_choose"), langPickerKeyboard())
	}
	code := strings.ToLower(strings.TrimSpace(args[0]))
	if !i18n.IsSupported(code) {
		return c.Send(i18n.T(current, "lang.unsupported", code, i18n.SupportedList()))
	}
	return b.applyLang(c, userID, code)
}

func (b *Bot) handleHelp(c tele.Context) error {
	return c.Send(i18n.T(b.UserLang(c.Sender().ID), "help.body"))
}

// handleText dispatches non-command text: pending state first, then menu labels.
func (b *Bot) handleText(c tele.Context) error {
	userID := c.Sender().ID
	lang := b.UserLang(userID)
	text := strings.TrimSpace(c.Text())

	p := b.peekPending(userID)
	if p.kind != pendingNone {
		return b.handlePendingInput(c, userID, lang, p, text)
	}

	if action, ok := b.menuLabelAction[text]; ok {
		switch action {
		case actMenuAdd:
			return b.handleAddURL(c)
		case actMenuList:
			return b.handleList(c)
		case actMenuLang:
			return c.Send(i18n.T(lang, "menu.lang_choose"), langPickerKeyboard())
		case actMenuHelp:
			return c.Send(i18n.T(lang, "help.body"))
		}
	}
	return nil
}

func (b *Bot) handlePendingInput(c tele.Context, userID int64, lang string, p pendingInput, text string) error {
	switch p.kind {
	case pendingAddURL:
		b.takePending(userID)
		if !isValidURL(text) {
			b.setPending(userID, pendingInput{kind: pendingAddURL})
			return c.Send(i18n.T(lang, "addurl.invalid_url"))
		}
		localID, err := b.store.AddWatch(userID, text)
		if err != nil {
			b.log.Error("addWatch failed", logger.Err(err))
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		b.super.Spawn(b.ctx, userID, text)
		return c.Send(i18n.T(lang, "addurl.success", localID))

	case pendingFilterPrice:
		b.takePending(userID)
		lo, hi, ok := domain.ParsePriceRange(text)
		if !ok {
			return c.Send(i18n.T(lang, "filter.bad_price"))
		}
		updated, err := b.store.SetPriceFilter(userID, p.localID, lo, hi)
		if err != nil {
			b.log.Warn("setPriceFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", p.localID))
		}
		return c.Send(i18n.T(lang, "filter.price_set", p.localID, formatBound(lo), formatBound(hi)))

	case pendingFilterInclude:
		b.takePending(userID)
		words := domain.NormalizeKeywords(strings.Split(text, ","))
		if len(words) == 0 {
			return c.Send(i18n.T(lang, "filter.usage"))
		}
		updated, err := b.store.SetIncludeFilter(userID, p.localID, words)
		if err != nil {
			b.log.Warn("setIncludeFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", p.localID))
		}
		return c.Send(i18n.T(lang, "filter.include_set", p.localID, strings.Join(words, ", ")))

	case pendingFilterExclude:
		b.takePending(userID)
		words := domain.NormalizeKeywords(strings.Split(text, ","))
		if len(words) == 0 {
			return c.Send(i18n.T(lang, "filter.usage"))
		}
		updated, err := b.store.SetExcludeFilter(userID, p.localID, words)
		if err != nil {
			b.log.Warn("setExcludeFilter failed", "err", err.Error())
			return c.Send(i18n.T(lang, "addurl.save_failed"))
		}
		if !updated {
			return c.Send(i18n.T(lang, "filter.target_not_found", p.localID))
		}
		return c.Send(i18n.T(lang, "filter.exclude_set", p.localID, strings.Join(words, ", ")))
	}
	return nil
}

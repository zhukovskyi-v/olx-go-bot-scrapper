package telegram

import (
	"github.com/fentezi/olx-scraper/internal/i18n"
	tele "gopkg.in/telebot.v3"
)

// UserLang returns the cached UI language for a user, loading from the store
// on first access. Falls back to i18n.DefaultLang.
func (b *Bot) UserLang(userID int64) string {
	if v, ok := b.langCache.Load(userID); ok {
		return v.(string)
	}
	lang, err := b.store.GetUserLanguage(userID)
	if err != nil || lang == "" || !i18n.IsSupported(lang) {
		lang = i18n.DefaultLang
	}
	b.langCache.Store(userID, lang)
	return lang
}

func (b *Bot) applyLang(c tele.Context, userID int64, code string) error {
	if err := b.store.SetUserLanguage(userID, code); err != nil {
		b.log.Warn("setUserLanguage failed", "err", err.Error())
		return c.Send(i18n.T(b.UserLang(userID), "addurl.save_failed"))
	}
	b.langCache.Store(userID, code)
	return c.Send(i18n.T(code, "lang.set", code), buildReplyKeyboard(code))
}

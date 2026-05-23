package telegram

import (
	"github.com/fentezi/olx-scraper/internal/i18n"
	tele "gopkg.in/telebot.v3"
)

// Menu action keys (mapped to localized button labels at startup).
const (
	actMenuAdd  = "add"
	actMenuList = "list"
	actMenuLang = "language"
	actMenuHelp = "help"
)

func (b *Bot) initMenuLabels() {
	pairs := []struct {
		key    string
		action string
	}{
		{"menu.add", actMenuAdd},
		{"menu.list", actMenuList},
		{"menu.language", actMenuLang},
		{"menu.help", actMenuHelp},
	}
	for _, lang := range i18n.Supported {
		for _, p := range pairs {
			b.menuLabelAction[i18n.T(lang, p.key)] = p.action
		}
	}
}

func (b *Bot) sendWithMenu(c tele.Context, lang, text string) error {
	return c.Send(text, buildReplyKeyboard(lang))
}

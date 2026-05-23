package telegram

import (
	"strconv"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
	tele "gopkg.in/telebot.v3"
)

func buildReplyKeyboard(lang string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{ResizeKeyboard: true}
	m.Reply(
		m.Row(m.Text(i18n.T(lang, "menu.add")), m.Text(i18n.T(lang, "menu.list"))),
		m.Row(m.Text(i18n.T(lang, "menu.language")), m.Text(i18n.T(lang, "menu.help"))),
	)
	return m
}

func rowKeyboard(w domain.Watch, lang string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	idStr := strconv.Itoa(w.LocalID)
	pauseLabel := i18n.T(lang, "row.pause")
	pauseUnique := cbWatchPause
	if w.Paused {
		pauseLabel = i18n.T(lang, "row.resume")
		pauseUnique = cbWatchResume
	}
	m.Inline(
		m.Row(
			tele.Btn{Unique: pauseUnique, Text: pauseLabel, Data: idStr},
			tele.Btn{Unique: cbWatchRemove, Text: i18n.T(lang, "row.remove"), Data: idStr},
			tele.Btn{Unique: cbWatchFilter, Text: i18n.T(lang, "row.filter"), Data: idStr},
		),
	)
	return m
}

func filterWizardKeyboard(localID int, lang string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	idStr := strconv.Itoa(localID)
	m.Inline(
		m.Row(
			tele.Btn{Unique: cbFilterPrice, Text: i18n.T(lang, "wiz.btn_price"), Data: idStr},
		),
		m.Row(
			tele.Btn{Unique: cbFilterInc, Text: i18n.T(lang, "wiz.btn_include"), Data: idStr},
			tele.Btn{Unique: cbFilterExc, Text: i18n.T(lang, "wiz.btn_exclude"), Data: idStr},
		),
		m.Row(
			tele.Btn{Unique: cbFilterClear, Text: i18n.T(lang, "wiz.btn_clear"), Data: idStr},
			tele.Btn{Unique: cbFilterCancel, Text: i18n.T(lang, "wiz.btn_cancel"), Data: idStr},
		),
	)
	return m
}

func langPickerKeyboard() *tele.ReplyMarkup {
	labels := map[string]string{
		i18n.LangUK: "🇺🇦 Українська",
		i18n.LangRU: "🇷🇺 Русский",
		i18n.LangEN: "🇬🇧 English",
		i18n.LangPL: "🇵🇱 Polski",
	}
	m := &tele.ReplyMarkup{}
	row := m.Row(
		tele.Btn{Unique: cbLangSet, Text: labels[i18n.LangUK], Data: i18n.LangUK},
		tele.Btn{Unique: cbLangSet, Text: labels[i18n.LangRU], Data: i18n.LangRU},
	)
	row2 := m.Row(
		tele.Btn{Unique: cbLangSet, Text: labels[i18n.LangEN], Data: i18n.LangEN},
		tele.Btn{Unique: cbLangSet, Text: labels[i18n.LangPL], Data: i18n.LangPL},
	)
	m.Inline(row, row2)
	return m
}

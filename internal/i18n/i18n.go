package i18n

import (
	"fmt"
	"strings"
)

const (
	LangUK = "uk"
	LangRU = "ru"
	LangEN = "en"
	LangPL = "pl"

	DefaultLang = LangUK
)

var Supported = []string{LangUK, LangRU, LangEN, LangPL}

func IsSupported(code string) bool {
	for _, c := range Supported {
		if c == code {
			return true
		}
	}
	return false
}

// NormalizeLang maps Telegram-style codes ("uk-UA", "ru", "en-US") to a supported
// language. Unknown codes fall back to DefaultLang.
func NormalizeLang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return DefaultLang
	}
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	if IsSupported(code) {
		return code
	}
	return DefaultLang
}

// T returns the localized template for key, formatted with args via fmt.Sprintf.
// Falls back to DefaultLang if the key is missing in lang; if also missing there,
// returns the key itself so missing strings are loud.
func T(lang, key string, args ...any) string {
	tmpl, ok := messages[lang][key]
	if !ok {
		tmpl = messages[DefaultLang][key]
	}
	if tmpl == "" {
		return key
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

func SupportedList() string {
	return strings.Join(Supported, ", ")
}

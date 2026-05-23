package i18n

import "testing"

func TestAllKeysPresent(t *testing.T) {
	reference := messages[DefaultLang]
	for _, lang := range Supported {
		got := messages[lang]
		if got == nil {
			t.Fatalf("language %q missing from catalog", lang)
		}
		for key := range reference {
			if _, ok := got[key]; !ok {
				t.Errorf("language %q missing key %q", lang, key)
			}
		}
		for key := range got {
			if _, ok := reference[key]; !ok {
				t.Errorf("language %q has extra key %q (not in %s)", lang, key, DefaultLang)
			}
		}
	}
}

func TestNormalizeLang(t *testing.T) {
	cases := map[string]string{
		"":      DefaultLang,
		"uk":    "uk",
		"uk-UA": "uk",
		"ru-RU": "ru",
		"en":    "en",
		"en-US": "en",
		"pl":    "pl",
		"xx":    DefaultLang,
		"  UK ": "uk",
	}
	for in, want := range cases {
		if got := NormalizeLang(in); got != want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestT_FallbackToDefault(t *testing.T) {
	got := T("xx", "lang.set", "uk")
	if got == "lang.set" {
		t.Error("expected fallback to default lang, got raw key")
	}
}

func TestT_MissingKeyReturnsKey(t *testing.T) {
	got := T(LangUK, "nonexistent.key")
	if got != "nonexistent.key" {
		t.Errorf("missing key should return key itself, got %q", got)
	}
}

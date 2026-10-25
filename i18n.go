package main

// UI language. Leonzilla is written in Czech; English texts come from enCatalog (i18n_en.go),
// keyed by the Czech text (for messages with values: the Czech format string).
// T returns the text in the current language – a missing translation falls back to Czech.

import (
	"os"
	"strings"
	"sync/atomic"
)

var curLang atomic.Value // "cs" | "en"

// resolveLang turns the setting ("" = automatic) into the language used.
func resolveLang(setting string) string {
	switch setting {
	case "cs", "en":
		return setting
	}
	if l := os.Getenv("LEONZILLA_LANG"); l == "cs" || l == "en" {
		return l
	}
	if osLangCzech() {
		return "cs"
	}
	return "en"
}

func setLang(setting string) { curLang.Store(resolveLang(setting)) }

func lang() string {
	if l, ok := curLang.Load().(string); ok {
		return l
	}
	return "cs"
}

// T translates a Czech UI text / format string to the current language.
func T(cz string) string {
	if lang() == "en" {
		if en, ok := enCatalog[cz]; ok {
			return en
		}
	}
	return cz
}

// envLangCzech: Czech / Slovak locale in the usual environment variables (non-Windows).
func envLangCzech() bool {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(os.Getenv(k)); v != "" {
			return strings.HasPrefix(v, "cs") || strings.HasPrefix(v, "sk")
		}
	}
	return false
}

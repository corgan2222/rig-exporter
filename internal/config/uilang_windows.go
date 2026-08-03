//go:build windows

package config

import (
	"strings"

	"golang.org/x/sys/windows"
)

// osLanguage is the language Windows itself is displayed in, as a bare code
// such as "de" or "en".
//
// A fresh installation used to start in German whatever the machine spoke,
// which made the English catalogue unreachable unless the user found the
// switcher first. Only the default is derived from this: once a language is
// written to config.json it is the user's choice and is never overridden.
//
// The preferred-languages list is asked rather than the single default UI
// language, because it is the list a user actually curates, and its first
// entry is what the rest of Windows shows them.
func osLanguage() string {
	languages, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return ""
	}
	for _, tag := range languages {
		// The tags are RFC 4646 names such as "de-DE"; the region is of no
		// interest here, only the language.
		if code, _, _ := strings.Cut(tag, "-"); code != "" {
			return code
		}
	}
	return ""
}

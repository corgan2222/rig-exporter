// Package i18n carries every user-facing string in more than one language.
//
// Two mechanisms, one type. Text holds the translations of a single string and
// is embedded directly where the string is defined — a metric name lives next
// to the metric, not in a distant table. The catalogue in strings.go holds the
// interface text, which has no natural home other than the screen it appears
// on.
//
// Machine-facing text is deliberately not translated: Prometheus help strings,
// log messages and error values stay English, because they are read by tools
// and by whoever is debugging, not by the person looking at the tray.
package i18n

import "strings"

// Lang identifies a supported language.
type Lang string

const (
	// DE is German, the default.
	DE Lang = "de"
	// EN is English.
	EN Lang = "en"
)

// Default is used when nothing has been configured.
const Default = DE

// Language pairs a code with its own name, for the language switcher.
type Language struct {
	Code Lang
	// Name is the language's name in itself, which is what a switcher should
	// show: someone looking for English does not read "Englisch".
	Name string
}

// Available lists the supported languages in presentation order.
var Available = []Language{
	{DE, "Deutsch"},
	{EN, "English"},
}

// Parse turns a configured or submitted value into a language, falling back to
// the default for anything unrecognised.
func Parse(value string) Lang {
	switch Lang(strings.ToLower(strings.TrimSpace(value))) {
	case EN:
		return EN
	case DE:
		return DE
	default:
		return Default
	}
}

// Text is one string in every supported language.
type Text struct {
	DE string
	EN string
}

// In returns the translation, falling back to German when a translation is
// missing so a half-finished catalogue degrades to readable rather than empty.
func (t Text) In(lang Lang) string {
	if lang == EN && t.EN != "" {
		return t.EN
	}
	return t.DE
}

// Empty reports whether no translation was provided at all.
func (t Text) Empty() bool { return t.DE == "" && t.EN == "" }

// T looks an interface string up in the catalogue.
//
// An unknown key returns the key itself. That makes a missing translation
// obvious on screen without crashing a page or hiding a control.
func T(lang Lang, key string) string {
	text, ok := catalogue[key]
	if !ok {
		return key
	}
	return text.In(lang)
}

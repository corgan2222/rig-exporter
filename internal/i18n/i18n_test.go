package i18n

import (
	"strings"
	"testing"
)

func TestParseFallsBackToTheDefault(t *testing.T) {
	cases := map[string]Lang{
		"de":      DE,
		"en":      EN,
		"  EN  ":  EN,
		"De":      DE,
		"":        Default,
		"klingon": Default,
	}
	for in, want := range cases {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextFallsBackToGerman(t *testing.T) {
	both := Text{DE: "Auflösung", EN: "Resolution"}
	if got := both.In(EN); got != "Resolution" {
		t.Errorf("In(EN) = %q", got)
	}
	if got := both.In(DE); got != "Auflösung" {
		t.Errorf("In(DE) = %q", got)
	}

	// A missing translation must read as German rather than as nothing.
	partial := Text{DE: "Belegung"}
	if got := partial.In(EN); got != "Belegung" {
		t.Errorf("In(EN) with no English = %q, want the German", got)
	}
	if !(Text{}).Empty() {
		t.Error("an empty Text does not report itself as empty")
	}
}

// A missing key has to be visible on screen, not silently blank: an empty
// button or label would be much harder to notice than a stray key.
func TestUnknownKeyReturnsTheKey(t *testing.T) {
	if got := T(DE, "does.not.exist"); got != "does.not.exist" {
		t.Errorf("T = %q, want the key back", got)
	}
}

// Every catalogue entry must exist in both languages, or an English interface
// shows German in the middle of it.
func TestCatalogueIsComplete(t *testing.T) {
	for key, text := range catalogue {
		if text.DE == "" {
			t.Errorf("%s has no German text", key)
		}
		if text.EN == "" {
			t.Errorf("%s has no English text", key)
		}
	}
}

// Catalogue entries reach the templates through two different paths: T
// escapes, H does not. Anything containing markup must be fetched with H, and
// the only markup allowed is the inline formatting used in hints.
func TestOnlyExpectedMarkupIsUsed(t *testing.T) {
	allowed := []string{"<code>", "</code>", "&lt;", "&gt;", "&amp;", "&nbsp;"}

	for key, text := range catalogue {
		for _, value := range []string{text.DE, text.EN} {
			stripped := value
			for _, tag := range allowed {
				stripped = strings.ReplaceAll(stripped, tag, "")
			}
			if strings.ContainsAny(stripped, "<>") {
				t.Errorf("%s contains unexpected markup: %q", key, value)
			}
		}
	}
}

func TestAvailableListsEveryLanguage(t *testing.T) {
	seen := map[Lang]bool{}
	for _, language := range Available {
		if language.Name == "" {
			t.Errorf("%q has no display name", language.Code)
		}
		if seen[language.Code] {
			t.Errorf("duplicate language %q", language.Code)
		}
		seen[language.Code] = true
	}
	if !seen[DE] || !seen[EN] {
		t.Errorf("Available is missing a language: %v", Available)
	}
}

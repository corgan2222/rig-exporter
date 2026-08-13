package webui

import (
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
)

// The handbook link is in the layout, so it is on all four pages or on none.
// Checking one page would not tell the two apart.
func TestTheHandbookIsLinkedFromEveryPage(t *testing.T) {
	_, ts := newServer(t, func(c *config.Config) { c.Language = string(i18n.EN) })

	for _, path := range []string{"/", "/capture", "/measurements", "/export"} {
		_, body := get(t, ts.URL+path)
		if !strings.Contains(body, `href="`+config.DocsURL+`"`) {
			t.Errorf("%s does not link the handbook", path)
		}
		if !strings.Contains(body, ">Help<") {
			t.Errorf("%s carries the address but not the word", path)
		}
	}
}

// The handbook exists in both languages, and a reader who set the interface to
// German has said which one they want. English is the site's default and lives
// at the root, so the German address is the one that needs a folder — get that
// backwards and every German reader lands on an English page.
func TestTheHandbookLinkFollowsTheInterfaceLanguage(t *testing.T) {
	for _, want := range []struct {
		lang i18n.Lang
		url  string
	}{
		{i18n.EN, config.DocsURL},
		{i18n.DE, config.DocsURL + "de/"},
	} {
		_, ts := newServer(t, func(c *config.Config) { c.Language = string(want.lang) })
		_, body := get(t, ts.URL+"/")

		if !strings.Contains(body, `href="`+want.url+`"`) {
			t.Errorf("%s links somewhere other than %s", want.lang, want.url)
		}
	}
}

// A language the site does not build has to land on a page rather than on a
// folder that was never generated: a page in the wrong language beats a 404 in
// the right one.
func TestAnUnbuiltLanguageFallsBackToTheRoot(t *testing.T) {
	if got := config.DocsFor(i18n.Lang("fr")); got != config.DocsURL {
		t.Errorf("DocsFor(fr) = %q, want the root %q", got, config.DocsURL)
	}
}

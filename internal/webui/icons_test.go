//go:build windows

package webui

import (
	"regexp"
	"strings"
	"testing"
)

// iconsIn collects the names of the symbols a page draws.
func iconsIn(body string) map[string]int {
	found := map[string]int{}
	for _, match := range regexp.MustCompile(`data-icon="([a-z]+)"`).FindAllStringSubmatch(body, -1) {
		found[match[1]]++
	}
	return found
}

// The same action carries the same symbol on both pages.
//
// This is the whole point of them being template definitions rather than two
// pieces of markup: a symbol somebody has to learn twice is a symbol that gets
// learned once and then guessed at. The test is what keeps the two pages from
// drifting apart the next time one of them is edited.
func TestTheSameActionCarriesTheSameSymbolOnBothPages(t *testing.T) {
	crash := "rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log"
	ownLogDir(t, "rig-exporter.log", crash)
	server, ts := newServer(t, nil)
	server.app.SetCrash(panicReport())

	_, dashboard := get(t, ts.URL+"/")
	_, export := get(t, ts.URL+"/export")

	onBoth := []string{"view", "download", "github"}
	for _, name := range onBoth {
		if iconsIn(dashboard)[name] == 0 {
			t.Errorf("the dashboard has no %q symbol", name)
		}
		if iconsIn(export)[name] == 0 {
			t.Errorf("the export page has no %q symbol", name)
		}
	}
	// Putting a notice away only exists where the notice is.
	if iconsIn(dashboard)["read"] == 0 {
		t.Error("the crash banner has no way to say it has been read")
	}
}

// A symbol without a word is a puzzle. Every one of these buttons says what it
// does, in the tooltip and to a screen reader.
func TestEverySymbolButtonSaysWhatItDoes(t *testing.T) {
	crash := "rig-exporter_corgan-pc3_crashreport_2026-08-07_15-40-00.log"
	ownLogDir(t, "rig-exporter.log", crash)
	server, ts := newServer(t, nil)
	server.app.SetCrash(panicReport())

	// The opening tag of every element that carries the symbol-button class.
	opening := regexp.MustCompile(`<(?:a|button)\s[^>]*class="iconbtn"[^>]*>`)

	for _, page := range []string{"/", "/export"} {
		_, body := get(t, ts.URL+page)
		tags := opening.FindAllString(body, -1)
		if len(tags) == 0 {
			t.Errorf("%s has no symbol buttons at all", page)
			continue
		}
		for _, tag := range tags {
			if !strings.Contains(tag, "title=") {
				t.Errorf("%s: a symbol button has no tooltip: %s", page, tag)
			}
			if !strings.Contains(tag, "aria-label=") {
				t.Errorf("%s: a symbol button is unnamed for a screen reader: %s", page, tag)
			}
		}
	}
}

// Filtering to errors must not leave the blank lines behind.
//
// It did, and it looked like a fault in the log rather than in the page: the
// line break sat outside the span, so hiding the line kept its break and the
// box filled up with empty rows. Measured on the rendered page, because that is
// where the two ways of writing it look identical in the template.
func TestFilteringToErrorsLeavesNoBlankLinesBehind(t *testing.T) {
	ownLogDir(t, "rig-exporter.log")
	_, ts := newServer(t, nil)

	_, body := get(t, ts.URL+"/export")

	start := strings.Index(body, `<pre class="logview">`)
	if start < 0 {
		t.Skip("no running log on this machine to render")
	}
	view := body[start:]
	view = view[:strings.Index(view, "</pre>")]

	// Every line break inside the view belongs to a line, so hiding the line
	// takes its break with it.
	if strings.Contains(view, "</span>\n") {
		t.Error("a line break sits outside its line and survives the filter")
	}
	if strings.Count(view, "<span") > 0 && !strings.Contains(view, "\n</span>") {
		t.Error("the lines carry no break at all")
	}
}

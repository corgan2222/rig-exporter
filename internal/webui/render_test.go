//go:build windows

package webui

import (
	"bytes"
	"html/template"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/i18n"
)

// Every template has to execute, for every page, in every language, from a
// pageData of nothing but zero values.
//
// The compiler does not check templates: a field removed from pageData while a
// template still names it builds fine and fails when somebody opens the page.
//
// TestEveryPageRenders next door does not catch that. It covers three of the
// four pages, one language, and asks only for a 200 — and render() logs a
// template error rather than failing the response, so a page that broke
// half-way through still answers 200 with the marker already written. This
// asserts on the execution error itself.
//
// Zero values on purpose: a template that only survives a populated struct is
// reaching for something it was never handed.
func TestEveryTemplateExecutesWithoutError(t *testing.T) {
	for _, page := range []string{"status", "capture", "measurements", "export"} {
		tmpl, err := template.ParseFS(templateFS,
			"templates/layout.html", "templates/"+page+".html")
		if err != nil {
			t.Errorf("parse %s: %v", page, err)
			continue
		}

		for _, language := range i18n.Available {
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, "layout", pageData{Lang: language.Code}); err != nil {
				t.Errorf("render %s in %s: %v", page, language.Code, err)
				continue
			}
			if buf.Len() == 0 {
				t.Errorf("render %s in %s produced nothing", page, language.Code)
			}
		}
	}
}

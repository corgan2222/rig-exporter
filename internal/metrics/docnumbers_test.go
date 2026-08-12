package metrics

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The documentation quotes numbers that come straight out of this catalogue —
// how many measurements each rung carries, how many land in the main area of a
// Home Assistant device, how many are diagnostic. They were written by hand and
// had drifted on every single one by the time anybody looked: 114 against 122,
// 72 against 76, 53 against 67, 34 against 48.
//
// They will drift again, because adding a measurement is one line here and
// nothing at all in the documentation. So the check lives next to the
// catalogue: add a measurement, and the run goes red until the handbook agrees.
//
// Both languages are checked, each against its own tree. Reading them into one
// blob was the first shape of this test, and it was a hole: the German sentence
// answered for the English one, so docs/en could say anything at all and the
// run stayed green. English is the source language — it is the tree that must
// never be the one that lags.
//
// Every occurrence is checked, not just the first one. That was the second
// hole: a figure is quoted in the README and again in docs/de, and a single
// FindStringSubmatch over the concatenated corpus stopped at the README copy —
// docs/de could carry a stale number untouched. Matches are counted per file so
// the failure can name the page that is wrong.
//
// It matches on the sentence rather than on a marker in the text, on purpose.
// A rewritten sentence should fail loudly — whoever rewrites it is exactly the
// person who should confirm the number is still true.
func TestTheHandbookNumbersMatchTheCatalogue(t *testing.T) {
	var (
		minimal    = len(PresetIDs(PresetMinimal))
		standard   = len(PresetIDs(PresetBasic))
		everything = len(All)
		primary    = countCategory("primary")
		diagnostic = countCategory("diagnostic")
		noEntity   = countNoEntity()
	)

	for _, corpus := range []struct {
		language string
		pages    []handbookPage
		checks   []numberCheck
	}{
		{
			// Each README belongs to the corpus of its own language. README.md
			// was the German one until the handbook moved into docs/; it is now
			// the English front page and README.de.md is the German one. Leaving
			// README.md in this corpus made it a page that could never match a
			// German sentence, and left README.de.md read by nothing at all.
			language: "de",
			pages:    readHandbook(t, "README.de.md", filepath.Join("docs", "de")),
			checks: []numberCheck{
				{"Minimal-Satz", regexp.MustCompile(`\*\*Minimal\*\* — (\d+) Messwerte`), minimal},
				{"Standard-Satz", regexp.MustCompile(`\*\*Standard\*\* — (\d+) Messwerte`), standard},
				{"Erweiterter Satz", regexp.MustCompile(`\*\*Erweitert\*\*[^—]*— alle (\d+)`), everything},
				{"Katalogumfang", regexp.MustCompile(`(\d+) Messwerte im Katalog`), everything},
				{"Hauptbereich", regexp.MustCompile(`(\d+) Messwerte stehen im Hauptbereich`), primary},
				{"Diagnose", regexp.MustCompile(`Hauptbereich, (\d+) unter`), diagnostic},
				{"ohne Entity", regexp.MustCompile(`(\d+) werden gar nicht\s+als Entität`), noEntity},
				// The README quotes the catalogue size in a sentence of its own.
				// It is the most-read page in the repository and was the one
				// page no pattern reached.
				{"README-Katalogumfang", regexp.MustCompile(`Jeder der (\d+) Messwerte`), everything},
			},
		},
		{
			language: "en",
			pages:    readHandbook(t, "README.md", filepath.Join("docs", "en")),
			checks: []numberCheck{
				{"minimal rung", regexp.MustCompile(`\*\*Minimal\*\* — (\d+) measurements`), minimal},
				{"standard rung", regexp.MustCompile(`\*\*Standard\*\* — (\d+) measurements`), standard},
				{"extended rung", regexp.MustCompile(`\*\*Extended\*\*[^—]*— all (\d+)`), everything},
				{"catalogue size", regexp.MustCompile(`(\d+) measurements in the catalogue`), everything},
				{"main area", regexp.MustCompile(`(\d+) measurements stand in the main area`), primary},
				{"diagnostic", regexp.MustCompile(`main area, (\d+) under`), diagnostic},
				{"without an entity", regexp.MustCompile(`(\d+) are not\s+published as an entity`), noEntity},
				{"README catalogue size", regexp.MustCompile(`Each of the (\d+) measurements`), everything},
			},
		},
	} {
		for _, c := range corpus.checks {
			matched := 0

			// Every page, and every occurrence within a page. A second copy of
			// the sentence answers for itself, not for the first one.
			for _, page := range corpus.pages {
				for _, match := range c.pattern.FindAllStringSubmatch(page.text, -1) {
					matched++
					got, err := strconv.Atoi(match[1])
					if err != nil {
						t.Errorf("%s/%s: %s quotes %q, which is not a number",
							corpus.language, c.what, page.path, match[1])
						continue
					}
					if got != c.want {
						t.Errorf("%s/%s: %s says %d, the catalogue has %d",
							corpus.language, c.what, page.path, got, c.want)
					}
				}
			}

			// An unmatched pattern is the failure mode this test exists to
			// prevent: it would quietly guard nothing at all.
			if matched == 0 {
				t.Errorf("%s/%s: no page states this sentence any more (pattern %q). "+
					"Whoever rewrote it should confirm the number and adjust the pattern.",
					corpus.language, c.what, c.pattern)
			}
		}
	}
}

// numberCheck is one sentence of the handbook that quotes a count, together
// with the count the catalogue actually holds.
type numberCheck struct {
	what    string
	pattern *regexp.Regexp
	want    int
}

// handbookPage is one documentation file. The pages are kept apart rather than
// joined into one string so that a failure can say which file is wrong, and so
// that a sentence appearing in two files is checked in both.
type handbookPage struct {
	path string // repo-relative, for the failure message
	text string
}

// readHandbook returns the named files and directories as one page per file; a
// directory contributes every .md file below it.
//
// A path that does not exist is skipped rather than fatal, so the README can
// eventually disappear into docs/ without this test standing in the way. An
// entirely empty corpus is still fatal — that is the case where the check would
// otherwise pass by having nothing to read.
func readHandbook(t *testing.T, paths ...string) []handbookPage {
	t.Helper()

	root := filepath.Join("..", "..")
	var pages []handbookPage

	// rel names a page the way the repository does, so the failure message
	// points at something the reader can open.
	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(r)
	}

	for _, path := range paths {
		full := filepath.Join(root, path)
		info, err := os.Stat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		if !info.IsDir() {
			raw, err := os.ReadFile(full)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			pages = append(pages, handbookPage{path: rel(full), text: string(raw)})
			continue
		}

		err = filepath.WalkDir(full, func(p string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(p, ".md") {
				return err
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			pages = append(pages, handbookPage{path: rel(p), text: string(raw)})
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	if len(pages) == 0 {
		t.Fatalf("no documentation found to check in %v", paths)
	}
	return pages
}

func countCategory(category string) int {
	n := 0
	for _, d := range All {
		if d.NoEntity {
			continue
		}
		if entityCategory(d) == category {
			n++
		}
	}
	return n
}

func countNoEntity() int {
	n := 0
	for _, d := range All {
		if d.NoEntity {
			n++
		}
	}
	return n
}

// entityCategory mirrors what the catalogue records, so the count here and the
// column there cannot disagree.
func entityCategory(d Definition) string {
	if d.EntityCategory != "" {
		return d.EntityCategory
	}
	return "primary"
}

package webui

import (
	"strings"
	"testing"
)

// The tile colouring lives in two files that know nothing about each other: the
// classes are declared in the stylesheet inside layout.html, and the script in
// status.html decides which one a reading earns. Either half is silently
// useless without the other — a class nobody sets, or a name no rule matches —
// and neither shows up as a failing test anywhere else, because the page still
// renders and every value still reads correctly. It is just permanently the
// wrong colour, which is exactly the sort of thing nobody notices in a
// screenshot.
//
// This walks the delivered page and checks that the two halves still name the
// same things. It also proves the script survives html/template's contextual
// escaping intact, which is the other way a change here could go quiet.
func TestTheTileColoursAreDeclaredAndSet(t *testing.T) {
	_, ts := newServer(t, nil)
	_, body := get(t, ts.URL+"/")

	for _, class := range []string{"lvl-ok", "lvl-warn", "lvl-deep"} {
		if !strings.Contains(body, ".tile .value."+class) {
			t.Errorf("the stylesheet declares no rule for %s", class)
		}
	}

	// The three names the script can hand to level(), spelled without the
	// prefix. A name with no matching rule paints nothing at all.
	for _, name := range []string{`"ok"`, `"warn"`, `"deep"`} {
		if !strings.Contains(body, "return "+name) {
			t.Errorf("fpsLevel never returns %s", name)
		}
	}

	// The rest of the mechanism, in the form it has to reach the browser in.
	for _, fragment := range []string{
		"function level(el, name)",
		"el.classList.remove(...LEVELS)",
		"level($(\"v-fps\"), s.fps_available ? fpsLevel(s.fps) : \"\")",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("the page does not carry %q", fragment)
		}
	}
}

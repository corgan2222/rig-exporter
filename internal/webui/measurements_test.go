package webui

import (
	"strings"
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The same reading has to look the same on the dashboard and on the
// measurements page. Two renderers for one value is exactly the arrangement
// that lets them drift apart, and they had.
func TestTheMeasurementsPageAgreesWithTheDashboard(t *testing.T) {
	cases := []struct {
		what    string
		reading metrics.Reading
	}{
		{"precision from the catalogue", metrics.Gauge(metrics.CPUTemperature, "", 45)},
		{"a boolean", metrics.Bool(metrics.BatteryAC, "", true)},
		{"a counter past the %g cliff", metrics.Gauge(metrics.BatteryCycles, "", 1e6)},
		{"text", metrics.Text(metrics.CPUModel, "", "AMD Ryzen 9 5950X")},
		{"a value with a unit", metrics.Gauge(metrics.CoolingFanSpeed, "", 1200)},
	}

	for _, c := range cases {
		want := formatValue(c.reading)
		if len([]rune(want)) > maxValueWidth {
			continue // the measurements page truncates on purpose
		}
		if got := readingText(c.reading); got != want {
			t.Errorf("%s (%s): measurements page %q, dashboard %q",
				c.what, c.reading.Def.ID, got, want)
		}
	}
}

// The three the review named, pinned individually so a failure says which rule
// broke rather than only that the two disagree.
func TestTheMeasurementsPageHonoursTheCatalogue(t *testing.T) {
	cases := []struct {
		what    string
		reading metrics.Reading
		want    string
	}{
		// EffectivePrecision is 1 for a temperature. fmt.Sprint never asks.
		{"precision", metrics.Gauge(metrics.CPUTemperature, "", 45), "45.0 °C"},
		// ON is the MQTT payload, which has no business on a web page.
		{"boolean", metrics.Bool(metrics.BatteryAC, "", true), "1"},
		// %g tips into exponent form here, which reads as a fault and is not one.
		{"the exponent cliff", metrics.Gauge(metrics.BatteryCycles, "", 1e6), "1000000"},
	}

	for _, c := range cases {
		if got := readingText(c.reading); got != c.want {
			t.Errorf("%s: readingText = %q, want %q", c.what, got, c.want)
		}
	}
}

// Truncation stays where it was, and it still cuts on runes: a degree sign is
// two bytes, and cutting between them writes a broken character to the page.
func TestALongValueIsStillCutOnRunes(t *testing.T) {
	long := strings.Repeat("°", maxValueWidth+10)
	got := readingText(metrics.Text(metrics.CPUModel, "", long))

	if runes := []rune(got); len(runes) > maxValueWidth {
		t.Errorf("readingText returned %d runes, want at most %d", len(runes), maxValueWidth)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("readingText = %q, want it to end in an ellipsis", got)
	}
	if !strings.ContainsRune(got, '°') || strings.Contains(got, "�") {
		t.Errorf("readingText = %q, want whole runes", got)
	}
}

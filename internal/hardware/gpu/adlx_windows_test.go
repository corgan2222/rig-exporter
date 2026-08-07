//go:build windows

package gpu

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// polaris is what a Radeon RX 570 actually answered on 07.08.2026, read
// straight out of ADLX. Hotspot temperature and voltage are absent because the
// card reports ADLX_NOT_SUPPORTED for both — Polaris has neither sensor — and
// that is the case these tests exist for.
func polaris() adlxCard {
	return adlxCard{
		Index:        0,
		Name:         "Radeon RX 570 Series",
		TempC:        55,
		hasTemp:      true,
		CoreClock:    1318,
		hasCoreClock: true,
		MemClock:     1750,
		hasMemClock:  true,
		PowerW:       51.34,
		hasPower:     true,
		FanRPM:       1102,
		hasFanRPM:    true,
		VRAMUsedMB:   2078,
		hasVRAMUsed:  true,
		VRAMTotalMB:  8192,
		hasVRAMTotal: true,
	}
}

// The whole point of the source: on a machine with nothing but the AMD driver,
// these readings exist where before there were none.
func TestADLXReportsWhatWindowsAloneCannotSee(t *testing.T) {
	set := &metrics.Set{}
	if !mergeFromADLX(set, []adlxCard{polaris()}, map[string]string{}) {
		t.Fatal("mergeFromADLX reported no readings")
	}

	assertTextReading(t, set, metrics.GPUName, "0", "Radeon RX 570 Series")
	assertNumberReading(t, set, metrics.GPUTemperature, "0", 55)
	assertNumberReading(t, set, metrics.GPUCoreClock, "0", 1318)
	assertNumberReading(t, set, metrics.GPUMemoryClock, "0", 1750)
	assertNumberReading(t, set, metrics.GPUFanRPM, "0", 1102)
	assertNumberReading(t, set, metrics.GPUVRAMUsed, "0", 2078)
	assertNumberReading(t, set, metrics.GPUVRAMTotal, "0", 8192)

	// Rounded to the precision the catalogue gives each measurement, which is
	// why the expectations here are not the raw 51.34 W and 25.366 %.
	assertNumberReading(t, set, metrics.GPUPower, "0", 51.3)
	assertNumberReading(t, set, metrics.GPUVRAMPercent, "0", 25.4)
}

// A sensor the card does not have produces no reading at all. A zero would
// claim the hotspot is at 0 °C and the card is running at 0 mV.
func TestADLXLeavesSensorsTheCardLacksOut(t *testing.T) {
	set := &metrics.Set{}
	mergeFromADLX(set, []adlxCard{polaris()}, map[string]string{})

	for _, def := range []metrics.Definition{metrics.GPUHotspot, metrics.GPUVoltage} {
		if _, exists := set.Find(def.ID, "0"); exists {
			t.Errorf("%s was published although the card reports it as unsupported", def.ID)
		}
	}
}

// gpu_fan is the fan's duty cycle. ADLX publishes the tachometer and nothing
// else, at any version, so dividing the tachometer by a range maximum would put
// a different quantity under an established identifier — the reading would look
// comparable to the NVIDIA one and would not be.
func TestADLXNeverInventsAFanPercentage(t *testing.T) {
	card := polaris()
	card.FanRPM, card.hasFanRPM = 4500, true // the top of the range ADLX reports

	set := &metrics.Set{}
	mergeFromADLX(set, []adlxCard{card}, map[string]string{})

	if _, exists := set.Find(metrics.GPUFan.ID, "0"); exists {
		t.Error("gpu_fan was derived from the tachometer")
	}
	assertNumberReading(t, set, metrics.GPUFanRPM, "0", 4500)
}

// gpu_load stays with the Windows counters on AMD. ADLX answered 1 % on an
// RX 570 whose 3D engine counter stood at 39.6 %, because its GPUUsage is an
// instantaneous sample rather than an average over the sampling window. A
// vendor source may take a reading off the counters only by measuring it
// better, and this one measures it worse.
func TestADLXLeavesOverallLoadToTheWindowsCounters(t *testing.T) {
	set := &metrics.Set{}
	mergeFromADLX(set, []adlxCard{polaris()}, map[string]string{})

	if _, exists := set.Find(metrics.GPULoad.ID, "0"); exists {
		t.Error("ADLX published gpu_load and would displace the counter reading")
	}
}

// ADLX runs after Afterburner, and the cheapest source that already answered
// keeps the reading. Anything else would make the published number depend on
// which sources happen to be installed.
func TestADLXDoesNotOverwriteASourceThatSpokeFirst(t *testing.T) {
	set := &metrics.Set{}
	set.Origin = "MSI Afterburner"
	set.Add(
		metrics.Gauge(metrics.GPUTemperature, "0", 61),
		metrics.Gauge(metrics.GPUCoreClock, "0", 1244),
	)

	mergeFromADLX(set, []adlxCard{polaris()}, map[string]string{"0": "Radeon RX 570 Series"})

	assertNumberReading(t, set, metrics.GPUTemperature, "0", 61)
	assertNumberReading(t, set, metrics.GPUCoreClock, "0", 1244)
	// The gaps Afterburner left are still filled.
	assertNumberReading(t, set, metrics.GPUFanRPM, "0", 1102)

	if got := countReadings(set, metrics.GPUTemperature.ID, "0"); got != 1 {
		t.Errorf("%d temperature readings for card 0, want 1", got)
	}
}

// DXGI names the card first. The ADLX reading has to land on that same
// instance, or the dashboard grows a second card describing the same silicon.
func TestADLXJoinsTheInstanceDXGIAlreadyNamed(t *testing.T) {
	set := &metrics.Set{}
	set.Add(metrics.Text(metrics.GPUName, "0", "Radeon RX 570 Series"))
	known := map[string]string{"0": "Radeon RX 570 Series"}

	mergeFromADLX(set, []adlxCard{polaris()}, known)

	assertNumberReading(t, set, metrics.GPUTemperature, "0", 55)
	if got := countReadings(set, metrics.GPUName.ID, "0"); got != 1 {
		t.Errorf("%d name readings for card 0, want 1", got)
	}
}

// A card ADLX cannot get any current reading for still exists. It contributes
// nothing rather than a row of zeroes, and must not stop the cards after it.
func TestADLXKeepsGoingWhenOneCardAnswersNothing(t *testing.T) {
	silent := adlxCard{Index: 0, Name: "Radeon RX 6800 XT"}
	set := &metrics.Set{}

	mergeFromADLX(set, []adlxCard{silent, polaris()}, map[string]string{})

	if _, exists := set.Find(metrics.GPUTemperature.ID, "0"); exists {
		t.Error("the silent card published a temperature")
	}
	assertNumberReading(t, set, metrics.GPUTemperature, "1", 55)
}

// Every reading ADLX contributes has to say so, because the interface uses the
// origin to tell an AMD driver that is answering from one that is not.
func TestADLXLabelsItsReadingsAsComingFromTheDriver(t *testing.T) {
	set := &metrics.Set{}
	mergeFromADLX(set, []adlxCard{polaris()}, map[string]string{})

	reading, ok := set.Find(metrics.GPUTemperature.ID, "0")
	if !ok {
		t.Fatal("no temperature reading")
	}
	if reading.Origin != ADLXOrigin {
		t.Errorf("origin = %q, want %q", reading.Origin, ADLXOrigin)
	}
	if set.Origin == ADLXOrigin {
		t.Error("the set was left with the ADLX origin; later sources would be mislabelled")
	}
}

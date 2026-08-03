package pawnio

import (
	"math"
	"testing"
)

// encodeZen builds a raw thermal register the way the hardware would, so the
// tests read as temperatures rather than as hex.
func encodeZen(celsius float64, shifted bool) uint64 {
	if shifted {
		celsius += rangeOffsetC
	}
	raw := uint64(math.Round(celsius*8)) << 21
	if shifted {
		raw |= rangeSelect
	}
	return raw
}

func TestZenTemperatureDecodesBothScales(t *testing.T) {
	for _, tc := range []struct {
		name    string
		celsius float64
		shifted bool
	}{
		{"idle, direct scale", 38.5, false},
		{"loaded, direct scale", 89.0, false},
		{"idle, shifted scale", 38.5, true},
		{"loaded, shifted scale", 89.0, true},
		{"eighth of a degree", 45.125, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DecodeZenTemperature(encodeZen(tc.celsius, tc.shifted), "AMD Ryzen 9 5950X 16-Core Processor")
			if !ok {
				t.Fatal("the reading was rejected")
			}
			if math.Abs(got-tc.celsius) > 0.13 {
				t.Errorf("decoded %.3f, want %.3f", got, tc.celsius)
			}
		})
	}
}

// The shifted scale is signalled two ways, and either one counts. Missing the
// second would report a temperature 49 degrees too high — plausible enough to
// go unnoticed on a loaded machine.
func TestEitherFlagMeansTheShiftedScale(t *testing.T) {
	const want = 60.0

	viaRange := uint64(math.Round((want+rangeOffsetC)*8))<<21 | rangeSelect
	viaTj := uint64(math.Round((want+rangeOffsetC)*8))<<21 | tjSelect

	for name, raw := range map[string]uint64{"range select": viaRange, "tj select": viaTj} {
		got, ok := DecodeZenTemperature(raw, "AMD Ryzen 9 5950X 16-Core Processor")
		if !ok {
			t.Errorf("%s: rejected", name)
			continue
		}
		if math.Abs(got-want) > 0.13 {
			t.Errorf("%s: decoded %.2f, want %.2f", name, got, want)
		}
	}
}

// Some early Zen parts inflate the temperature they report so one fan curve
// could cover a whole range. Undoing it is the difference between a number that
// means something and one that is 20 degrees out.
func TestTheModelsThatInflateTheirTemperatureAreCorrected(t *testing.T) {
	raw := encodeZen(80, false)

	for model, want := range map[string]float64{
		"AMD Ryzen 5 1600X Six-Core Processor":   60,
		"AMD Ryzen 7 1700X Eight-Core Processor": 60,
		"AMD Ryzen 7 1800X Eight-Core Processor": 60,
		// Both Threadripper generations inflate by the same 27 degrees, and
		// the prefix catches every part in each: 1900X through 1950X, 2920X
		// through 2990WX.
		"AMD Ryzen Threadripper 1950X 16-Core":   53,
		"AMD Ryzen Threadripper 2990WX 32-Core":  53,
		"AMD Ryzen 7 2700X Eight-Core Processor": 70,
		// A later Threadripper must not be caught by those prefixes.
		"AMD Ryzen Threadripper 3970X 32-Core": 80,
		"AMD Ryzen Threadripper 7980X 64-Core": 80,
		"AMD Ryzen 9 5950X 16-Core Processor":  80, // Zen 3 reports the truth
		"AMD Ryzen 9 7950X 16-Core Processor":  80,
		"AMD Ryzen 5 3600 6-Core Processor":    80,
	} {
		got, ok := DecodeZenTemperature(raw, model)
		if !ok {
			t.Errorf("%s: rejected", model)
			continue
		}
		if math.Abs(got-want) > 0.13 {
			t.Errorf("%s: decoded %.2f, want %.2f", model, got, want)
		}
	}
}

// A register that read as zero, or that decodes to something no processor could
// be, means the read failed. Reporting nothing beats reporting that.
func TestImplausibleReadingsAreRefused(t *testing.T) {
	for name, raw := range map[string]uint64{
		"empty register": 0,
		"far too hot":    uint64(200*8) << 21,
	} {
		if _, ok := DecodeZenTemperature(raw, "AMD Ryzen 9 5950X 16-Core Processor"); ok {
			t.Errorf("%s was accepted", name)
		}
	}

	// Below freezing, via the shifted scale applied to a tiny reading.
	if _, ok := DecodeZenTemperature(uint64(1)<<21|rangeSelect, "AMD Ryzen 9 5950X"); ok {
		t.Error("a sub-zero reading was accepted")
	}
}

func TestCCDRegisterFollowsTheGeneration(t *testing.T) {
	// Vermeer, which is the 5950X.
	if got := CCDTemperatureRegister(0x21, 0); got != ccdTemperatureVermeer {
		t.Errorf("Vermeer CCD0 = 0x%X, want 0x%X", got, ccdTemperatureVermeer)
	}
	// Raphael and Granite Ridge moved the block.
	for _, model := range []uint32{0x61, 0x44} {
		if got := CCDTemperatureRegister(model, 0); got != ccdTemperatureRaphael {
			t.Errorf("model 0x%X CCD0 = 0x%X, want 0x%X", model, got, ccdTemperatureRaphael)
		}
	}
	// Chiplets are four bytes apart.
	if got := CCDTemperatureRegister(0x21, 1); got != ccdTemperatureVermeer+4 {
		t.Errorf("CCD1 = 0x%X, want 0x%X", got, ccdTemperatureVermeer+4)
	}
}

func TestCCDTemperatureDecodes(t *testing.T) {
	// 60 °C: (raw * 125 - 305000) / 1000 = 60  ->  raw = 2920
	got, ok := DecodeCCDTemperature(2920)
	if !ok {
		t.Fatal("rejected")
	}
	if math.Abs(got-60) > 0.13 {
		t.Errorf("decoded %.2f, want 60", got)
	}

	// Only the low twelve bits belong to the reading.
	if withNoise, ok := DecodeCCDTemperature(0xFFFFF000 | 2920); !ok || math.Abs(withNoise-60) > 0.13 {
		t.Errorf("high bits leaked into the reading: %.2f (ok=%v)", withNoise, ok)
	}

	if _, ok := DecodeCCDTemperature(0); ok {
		t.Error("an unpopulated chiplet was accepted")
	}
}

func TestEnergyUnitIsANegativePowerOfTwo(t *testing.T) {
	// The usual value on Zen is an exponent of 16, so 1/65536 joules per count.
	unit, ok := DecodeEnergyUnit(16 << 8)
	if !ok {
		t.Fatal("rejected")
	}
	if math.Abs(unit-1.0/65536) > 1e-12 {
		t.Errorf("unit = %v, want 1/65536", unit)
	}

	if _, ok := DecodeEnergyUnit(0); ok {
		t.Error("an empty power-unit register was accepted")
	}
}

// The energy counter only ever counts up, so watts are a difference over time —
// and the counter is 32 bits, so it wraps rather than going backwards.
func TestPackageWattsFromTheEnergyCounter(t *testing.T) {
	const unit = 1.0 / 65536

	// 65536 counts is one joule; over one second that is one watt.
	if got, ok := PackageWatts(1000, 1000+65536*95, unit, 1); !ok || math.Abs(got-95) > 0.01 {
		t.Errorf("watts = %v (ok=%v), want 95", got, ok)
	}

	// A wrap must read as a small positive draw, not as a vast negative one.
	const wrap = uint64(1) << 32
	got, ok := PackageWatts(wrap-65536*10, 65536*10, unit, 1)
	if !ok {
		t.Fatal("a wrapped counter was rejected")
	}
	if math.Abs(got-20) > 0.01 {
		t.Errorf("across a wrap = %v, want 20", got)
	}

	for name, args := range map[string][4]float64{
		"no time passed":  {1000, 2000, unit, 0},
		"negative time":   {1000, 2000, unit, -1},
		"no unit":         {1000, 2000, 0, 1},
		"nothing counted": {1000, 1000, unit, 1},
	} {
		if _, ok := PackageWatts(uint64(args[0]), uint64(args[1]), args[2], args[3]); ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

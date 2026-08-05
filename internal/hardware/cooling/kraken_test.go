package cooling

import "testing"

// realKrakenZ3Report is a status report captured from an NZXT Kraken Z3
// (0x1E71:0x3008) on 05.08.2026. At that moment the machine's monitoring
// software showed 45.9 °C liquid, 2409 rpm pump, 814 rpm fans, 76 % pump duty
// and 39 % fan duty — which is what makes this a measurement rather than a
// transcription of somebody else's table.
var realKrakenZ3Report = []byte{
	0x75, 0x01, 0x33, 0x00, 0x43, 0x00, 0x0f, 0x51,
	0x39, 0x33, 0x38, 0x32, 0x37, 0x33, 0x01, 0x2d,
	0x09, 0x60, 0x09, 0x4c, 0x4c, 0x01, 0x02, 0x2e,
	0x03, 0x27, 0x28, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

func TestARealKrakenReportDecodesToWhatTheCoolerShowed(t *testing.T) {
	got, ok := decodeKrakenV3(0x3008, realKrakenZ3Report)
	if !ok {
		t.Fatal("a real status report was refused")
	}

	if got.Device != "NZXT Kraken Z3" {
		t.Errorf("device = %q", got.Device)
	}
	if got.Instance != "3008" {
		t.Errorf("instance = %q, want the product id", got.Instance)
	}
	if !got.HasLiquid || got.LiquidTemperature != 45.9 {
		t.Errorf("liquid = %v (%v), want 45.9", got.LiquidTemperature, got.HasLiquid)
	}
	if !got.HasPumpRPM || got.PumpRPM != 2400 {
		t.Errorf("pump = %d rpm, want 2400", got.PumpRPM)
	}
	if !got.HasPumpDuty || got.PumpDuty != 76 {
		t.Errorf("pump duty = %d %%, want 76", got.PumpDuty)
	}
	if !got.HasFanRPM || got.FanRPM != 814 {
		t.Errorf("fan = %d rpm, want 814", got.FanRPM)
	}
	if !got.HasFanDuty || got.FanDuty != 39 {
		t.Errorf("fan duty = %d %%, want 39", got.FanDuty)
	}
}

// The one mistake this protocol invites is reading the report one byte late,
// as if Windows had put a report id in front of it. That reading is not
// obviously wrong — it produces plausible-looking numbers — so the marker
// check has to be what rejects it.
func TestAReportShiftedByOneIsRefusedRatherThanDecoded(t *testing.T) {
	shifted := append([]byte{0x00}, realKrakenZ3Report...)
	if _, ok := decodeKrakenV3(0x3008, shifted); ok {
		t.Fatal("a report with a leading report id was decoded as if it were the body")
	}
}

func TestOnlyStatusReportsAreDecoded(t *testing.T) {
	firmware := make([]byte, len(realKrakenZ3Report))
	copy(firmware, realKrakenZ3Report)
	firmware[0] = 0x11 // the firmware-info answer, which the device also sends

	if _, ok := decodeKrakenV3(0x3008, firmware); ok {
		t.Error("a report that is not a status report was decoded as one")
	}
}

func TestAnUnknownProductIsNotGuessedAt(t *testing.T) {
	if _, ok := decodeKrakenV3(0x170E, realKrakenZ3Report); ok {
		t.Error("the Kraken X2, which speaks a different protocol, was decoded as a V3")
	}
}

func TestATruncatedReportIsRefusedRatherThanPaddedWithZeroes(t *testing.T) {
	if _, ok := decodeKrakenV3(0x3008, realKrakenZ3Report[:20]); ok {
		t.Error("a short report was decoded, so the fan figures came out of thin air")
	}
}

// A cooler that has not measured yet reports nought degrees. That is not a
// temperature, and publishing it would claim the loop is at freezing point.
func TestAnUnmeasuredTemperatureIsLeftOutRatherThanPublishedAsZero(t *testing.T) {
	cold := make([]byte, len(realKrakenZ3Report))
	copy(cold, realKrakenZ3Report)
	cold[15], cold[16] = 0, 0

	got, ok := decodeKrakenV3(0x3008, cold)
	if !ok {
		t.Fatal("the report was refused entirely")
	}
	if got.HasLiquid {
		t.Error("0 °C was published as a temperature")
	}
	if !got.HasPumpRPM {
		t.Error("the rest of the report was thrown away with it")
	}
}

// A fan that stands still is a fact, not a missing value: on a Kraken with no
// fans plugged into the pump head it stands still for ever.
func TestAStandingFanIsAReadingAndNotAnOmission(t *testing.T) {
	still := make([]byte, len(realKrakenZ3Report))
	copy(still, realKrakenZ3Report)
	still[23], still[24], still[25] = 0, 0, 0

	got, _ := decodeKrakenV3(0x3008, still)
	if !got.HasFanRPM || got.FanRPM != 0 {
		t.Errorf("fan = %d rpm (%v), want a reported nought", got.FanRPM, got.HasFanRPM)
	}
	if !got.HasFanDuty || got.FanDuty != 0 {
		t.Errorf("fan duty = %d %% (%v), want a reported nought", got.FanDuty, got.HasFanDuty)
	}
}

func TestAnImplausibleDutyIsDropped(t *testing.T) {
	odd := make([]byte, len(realKrakenZ3Report))
	copy(odd, realKrakenZ3Report)
	odd[19] = 200

	got, _ := decodeKrakenV3(0x3008, odd)
	if got.HasPumpDuty {
		t.Error("a duty cycle of 200 %% was published")
	}
	if !got.HasFanDuty {
		t.Error("the fan duty was dropped along with it")
	}
}

// Package cooling reads USB-attached cooling controllers: all-in-one water
// coolers, pumps and fan hubs that announce themselves as human interface
// devices and push a status report on their own.
//
// The protocols are not documented by their makers. They are read off
// LibreHardwareMonitor, which reverse-engineered them, and only the reading
// half is implemented here — nothing in this package ever writes to a pump.
//
// Only the NZXT Kraken V3 family has been tried against real hardware. Every
// other device in here is decoded from the same source and has never seen the
// device it claims to read, which is why the whole source is off by default
// and labelled as untested.
package cooling

// Reading is what one controller currently reports. A field is only set when
// the device actually supplies it — a missing value is left out, not zeroed,
// and the flags say which is which.
type Reading struct {
	// Device names the controller for the entity label, e.g. "NZXT Kraken Z3".
	Device string
	// Instance is what the entity key is built from, so it must not translate
	// and must survive a firmware update: the model name, which is a constant
	// in this file rather than anything the device sends.
	//
	// It was the product id in hex until it turned out what that looks like on
	// a dashboard — a tile labelled "3008", and an entity called
	// cooling_fan_speed_3008.
	Instance string

	LiquidTemperature float64
	HasLiquid         bool
	PumpRPM           int
	HasPumpRPM        bool
	PumpDuty          int
	HasPumpDuty       bool
	FanRPM            int
	HasFanRPM         bool
	FanDuty           int
	HasFanDuty        bool
}

// vendorNZXT is the USB vendor id every Kraken and Grid carries.
const vendorNZXT = 0x1E71

// krakenV3Products are the coolers that speak the protocol decoded below.
//
// One decoder covers six product ids because NZXT kept the report layout
// across the range. Only 0x3008 has been verified against hardware; the others
// are here because leaving them out would help nobody and the failure mode of
// a wrong guess is a rejected report, not a wrong value.
var krakenV3Products = map[uint16]string{
	0x2007: "NZXT Kraken X3",
	0x2014: "NZXT Kraken X3",
	0x3008: "NZXT Kraken Z3",
	0x300C: "NZXT Kraken 2023 Elite",
	0x300E: "NZXT Kraken 2023",
	0x3012: "NZXT Kraken Elite V2",
}

// krakenV3StatusLength is how long a status report is. Anything shorter cannot
// hold the fan figures at the far end and is refused rather than padded.
const krakenV3StatusLength = 26

// decodeKrakenV3 turns one status report into a reading.
//
// The layout, verified against a Kraken Z3 alongside its monitoring software,
// which showed 45.9 °C, ~2400 rpm pump, 814 rpm fans, 76 % and 39 %.
//
// The pump figure used to be written here as 2409 and as 2400 in the test, from
// the same recording. The recorded report decodes to 2400, so that is the
// number; 2409 was presumably read off the software a moment earlier or later,
// which for a pump speed is not the same moment at all. Two numbers that
// quietly disagreed, and the byte layout below is what settles it.
//
//	 0, 1   0x75 0x01, the marker of a status report
//	15, 16  liquid temperature, whole degrees and tenths
//	17, 18  pump speed in rpm, low byte first
//	19      pump duty in percent
//	23, 24  fan speed in rpm, low byte first
//	25      fan duty in percent
//
// The report arrives without a report id in front of it — byte zero is already
// 0x75 — which is worth stating because the alternative reading of the same
// bytes is off by one and yields 19465 rpm and 15.7 °C. The marker check is
// what keeps that mistake from ever reaching an entity.
func decodeKrakenV3(product uint16, report []byte) (Reading, bool) {
	name, known := krakenV3Products[product]
	if !known || len(report) < krakenV3StatusLength {
		return Reading{}, false
	}
	if report[0] != 0x75 || report[1] != 0x01 {
		// The device also sends firmware and configuration reports. They are
		// not errors, they are simply not this.
		return Reading{}, false
	}

	r := Reading{
		Device: name,
		// The model name, not the USB product id. The instance becomes the
		// entity id and the label a person reads, and "3008" is neither — it
		// said nothing on the dashboard and nothing in an automation. Two
		// coolers of the same model still collide, but they did under the
		// product id as well, so nothing is lost.
		Instance: name,

		LiquidTemperature: float64(report[15]) + float64(report[16])/10,
		HasLiquid:         true,
		PumpRPM:           int(report[18])<<8 | int(report[17]),
		HasPumpRPM:        true,
		PumpDuty:          int(report[19]),
		HasPumpDuty:       true,
		FanRPM:            int(report[24])<<8 | int(report[23]),
		HasFanRPM:         true,
		FanDuty:           int(report[25]),
		HasFanDuty:        true,
	}

	// A pump that reports 0 °C is a pump that has not measured yet. Every
	// other figure can legitimately be zero — a fan can stand still, and on a
	// Kraken with no fans attached it always does.
	if r.LiquidTemperature == 0 {
		r.HasLiquid = false
	}
	// Duty cycles are percentages. Anything else means the report was not what
	// we think it is, and reporting it would be worse than reporting nothing.
	if r.PumpDuty > 100 {
		r.HasPumpDuty = false
	}
	if r.FanDuty > 100 {
		r.HasFanDuty = false
	}
	return r, true
}

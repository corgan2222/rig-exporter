package pawnio

import "strings"

// Decoding AMD Zen thermal registers.
//
// The module PawnIO loads is only a primitive: it reads a register and hands
// back the value. Everything that turns those bits into a temperature lives
// here, and it is where the mistakes live too — a wrong shift or a missed
// offset produces a number that looks entirely plausible and is simply wrong.
// So it is kept apart from anything that touches hardware and tested on its
// own, because that is the only part of this feature that can be proven without
// a kernel driver.
//
// The layout follows Libre Hardware Monitor, which has been maintaining it
// across Zen generations for years. Its own source carries a note that the
// per-model offsets keep changing, which is the honest state of this: AMD never
// documented them.

const (
	// smnTemperature is SMU_THM_TCON_CUR_TMP, the current thermal reading.
	smnTemperature = 0x00059800

	// rangeSelect and tjSelect each mean the reading is on the shifted scale.
	rangeSelect = 0x80000
	tjSelect    = 0x30000

	// rangeOffsetC is what the shifted scale is offset by.
	rangeOffsetC = 49.0

	// milliPerStep is the temperature step: bits [31:21] count in eighths of a
	// degree, which is 125 thousandths.
	milliPerStep = 125
)

// tctlOffsets are the models whose reported Tctl is deliberately raised above
// the real junction temperature, so that one fan curve could suit a whole
// range. Undoing it is the only way to get a temperature that means anything.
//
// Matched on the brand string because AMD exposes no other way to tell these
// apart — the family and model are shared with parts that need no offset. Zen 2
// and everything after report the true value, so this list is closed unless AMD
// starts again.
var tctlOffsets = []struct {
	match  string
	offset float64
}{
	{"1600X", 20},
	{"1700X", 20},
	{"1800X", 20},
	{"Threadripper 19", 27},
	{"Threadripper 29", 27},
	{"2700X", 10},
}

// DecodeZenTemperature turns the raw thermal register into degrees Celsius.
//
// model is the processor's brand string, which decides whether an offset has to
// be undone. The second result is false when the register held nothing usable.
func DecodeZenTemperature(raw uint64, model string) (float64, bool) {
	if raw == 0 {
		return 0, false
	}

	celsius := float64((raw>>21)*milliPerStep) / 1000

	// Two independent ways of saying the same thing: the reading sits on a
	// scale shifted up by 49 degrees. Either flag counts.
	shifted := raw&rangeSelect != 0 || raw&tjSelect == tjSelect
	if shifted {
		celsius -= rangeOffsetC
	}

	celsius -= tctlOffset(model)

	// A processor below freezing or above its own melting point means the
	// register was not what we thought. Reporting nothing beats reporting that.
	if celsius < -10 || celsius > 125 {
		return 0, false
	}
	return celsius, true
}

// tctlOffset is how much this model inflates its reported temperature.
func tctlOffset(model string) float64 {
	for _, entry := range tctlOffsets {
		if strings.Contains(model, entry.match) {
			return entry.offset
		}
	}
	return 0
}

// Per-CCD temperatures. A chiplet processor has one of these per die, and they
// are the readings that show a single hot chiplet under a load that leaves the
// package average looking calm.
const (
	ccdTemperatureVermeer = 0x00059954 // and every Zen 2/3 part before Raphael
	ccdTemperatureRaphael = 0x00059b08 // Raphael and Granite Ridge

	ccdRawMask     = 0xFFF
	ccdOffsetMilli = 305000
)

// CCDTemperatureRegister is where a chiplet's temperature is read from.
//
// AMD moved the block between generations, and the two model numbers are the
// only way to tell which layout a part uses.
func CCDTemperatureRegister(model uint32, ccd int) uint32 {
	base := uint32(ccdTemperatureVermeer)
	if model == 0x61 || model == 0x44 {
		base = ccdTemperatureRaphael
	}
	return base + uint32(ccd)*4
}

// DecodeCCDTemperature turns a chiplet register into degrees Celsius. The
// second result is false for a chiplet that is not populated or not reporting.
func DecodeCCDTemperature(raw uint64) (float64, bool) {
	value := raw & ccdRawMask
	if value == 0 {
		return 0, false
	}

	celsius := (float64(value*milliPerStep) - ccdOffsetMilli) / 1000
	if celsius < -10 || celsius > 125 {
		return 0, false
	}
	return celsius, true
}

// Package power, which AMD reports as an ever-increasing energy counter rather
// than a rate: the watts are the difference between two readings divided by the
// time between them.
const (
	// MSRPowerUnit carries the scale the energy counter is expressed in.
	MSRPowerUnit = 0xC0010299
	// MSRPackageEnergy is the counter itself.
	MSRPackageEnergy = 0xC001029B
)

// DecodeEnergyUnit turns the power-unit register into joules per count.
func DecodeEnergyUnit(raw uint64) (float64, bool) {
	// Bits [12:8] hold the exponent of a negative power of two.
	exponent := (raw >> 8) & 0x1F
	if exponent == 0 {
		return 0, false
	}

	unit := 1.0
	for i := uint64(0); i < exponent; i++ {
		unit /= 2
	}
	return unit, true
}

// PackageWatts is the power drawn between two readings of the energy counter.
//
// The counter is 32 bits and wraps, so a reading lower than the one before it
// is a wrap rather than negative energy. Seconds must be positive; a zero
// interval would divide the difference by nothing.
func PackageWatts(previous, current uint64, unit, seconds float64) (float64, bool) {
	if seconds <= 0 || unit <= 0 {
		return 0, false
	}

	const wrap = 1 << 32
	delta := current - previous
	if current < previous {
		delta = current + wrap - previous
	}

	watts := float64(delta) * unit / seconds

	// Nothing on a desktop draws a kilowatt through the package. A figure that
	// large means the counter did something other than count.
	if watts <= 0 || watts > 1000 {
		return 0, false
	}
	return watts, true
}

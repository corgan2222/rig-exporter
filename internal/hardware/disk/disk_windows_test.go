//go:build windows

package disk

import "testing"

// A drive that reports no vendor leaves only its product string, and that is
// sometimes a brand and sometimes a part number. Publishing a part number under
// "manufacturer" is a wrong answer where none was available — measured on a real
// machine, where a WD drive reported WDS200T1X0E-00AFY0 and nothing else.
func TestOnlyABrandIsAcceptedAsAManufacturer(t *testing.T) {
	for product, want := range map[string]string{
		"Samsung SSD 980 PRO 1TB": "Samsung",
		"Seagate FireCuda 530":    "Seagate",
		"INTEL SSDPEKNW010T8":     "INTEL",
		"KINGSTON SA400S37240G":   "KINGSTON",

		// Part numbers, not manufacturers.
		"WDS200T1X0E-00AFY0": "",
		"CT1000P3PSSD8":      "",
		"970":                "",

		"":    "",
		"   ": "",
	} {
		if got := brandOf(product); got != want {
			t.Errorf("brandOf(%q) = %q, want %q", product, got, want)
		}
	}
}

// The descriptor's offsets come from the driver, so they are checked against the
// buffer rather than trusted — an offset past the end would otherwise read
// whatever memory follows.
func TestDescriptorStringStaysInsideTheBuffer(t *testing.T) {
	buf := []byte("....Samsung\x00trailing rubbish")

	if got := descriptorString(buf, 4); got != "Samsung" {
		t.Errorf("descriptorString = %q, want Samsung", got)
	}
	for _, offset := range []uint32{0, uint32(len(buf)), uint32(len(buf)) + 100, 1 << 30} {
		if got := descriptorString(buf, offset); got != "" {
			t.Errorf("offset %d returned %q, want nothing", offset, got)
		}
	}

	// A string running to the very end without a terminator must still stop.
	if got := descriptorString([]byte("Samsung"), 0); got != "" {
		t.Errorf("a zero offset means absent, got %q", got)
	}
	if got := descriptorString([]byte("xSamsung"), 1); got != "Samsung" {
		t.Errorf("unterminated string = %q, want Samsung", got)
	}
}

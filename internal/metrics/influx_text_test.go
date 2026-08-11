package metrics

import (
	"strings"
	"testing"
	"time"
)

// identityOf is the measurement and its tags — everything before the first
// space in a line of line protocol. In InfluxDB that is the series: two lines
// whose identities differ are two series, however much they look alike.
func identityOf(line string) string {
	return strings.SplitN(strings.TrimSpace(line), " ", 2)[0]
}

// A reading that is only text has to reach InfluxDB too.
//
// ram_module is text and carries an instance, and it is the only RAM
// definition that does — every other one lands on the point {ram, ""}. So
// every {ram, slot} point consisted of tags and nothing else and was dropped
// as invalid, and a catalogued reading that JSON and Prometheus both carry
// went missing from one export and from nowhere else. It is not the only one:
// a graphics card that reports nothing but gpu_name, a volume with only
// disk_label, a controller with only cooling_device. ram_module is the case
// that happens on every machine, every time.
func TestInfluxCarriesTextOnlyInstances(t *testing.T) {
	var set Set
	set.Add(
		Gauge(RAMUsed, "", 16384),
		Gauge(RAMModules, "", 2),
		Text(RAMModule, "dimm_a1", "Corsair 32 GB"),
		Text(RAMModule, "dimm_b1", "Corsair 32 GB"),
	)

	out := string(set.Influx("rig", "pc", time.Unix(0, 0)))

	for _, slot := range []string{"dimm_a1", "dimm_b1"} {
		if !strings.Contains(out, slot) {
			t.Errorf("slot %s never reaches the line protocol:\n%s", slot, out)
		}
	}
}

// The tag set is the series identity, so a text reading that moves at runtime
// must not be in it.
//
// The counters on the same point are cumulative. When the address changes, a
// tagged address starts a second series with its own lifetime totals, and a
// rate derived across the change tears. The code already knows this rule and
// states it ten lines earlier, for the table labels: a tag would be indexed,
// and every program that ever led the list would stay in that index forever.
// KindText had the opposite rule with no mention of the counters.
func TestInfluxKeepsTheAddressOutOfTheSeriesIdentity(t *testing.T) {
	line := func(ip string) string {
		var set Set
		set.Add(
			Text(NetIP, "Wi-Fi", ip),
			Gauge(NetRxTotal, "Wi-Fi", 812.44),
			Gauge(NetTxTotal, "Wi-Fi", 91.02),
		)
		return strings.TrimSpace(string(set.Influx("rig", "pc", time.Unix(0, 0))))
	}

	before, after := identityOf(line("192.168.1.42")), identityOf(line("10.0.0.5"))
	if before != after {
		t.Errorf("the series identity moved with the address:\n%s\n%s", before, after)
	}
}

// The text itself still has to arrive — it is a reading, not decoration.
func TestInfluxStillCarriesTheTextItself(t *testing.T) {
	var set Set
	set.Add(
		Text(NetIP, "Wi-Fi", "192.168.1.42"),
		Gauge(NetRxTotal, "Wi-Fi", 812.44),
	)

	out := string(set.Influx("rig", "pc", time.Unix(0, 0)))
	if !strings.Contains(out, "192.168.1.42") {
		t.Errorf("the address is not in the line at all:\n%s", out)
	}
}

// What stays a tag is what identifies the thing being measured, and nothing
// else: the host and the instance label.
func TestOnlyTheStableDimensionsStayTags(t *testing.T) {
	var set Set
	set.Add(
		Text(NetIP, "Wi-Fi", "192.168.1.42"),
		Gauge(NetRxTotal, "Wi-Fi", 812.44),
	)

	identity := identityOf(string(set.Influx("rig", "pc", time.Unix(0, 0))))

	if !strings.Contains(identity, "host=pc") || !strings.Contains(identity, "nic=Wi-Fi") {
		t.Errorf("the identity lost a dimension it needs: %s", identity)
	}
	if strings.Contains(identity, "ip=") {
		t.Errorf("the address is still part of the identity: %s", identity)
	}
}

// A string field is quoted and escaped, not written raw. An adapter is called
// whatever Windows calls it, and a quote or a backslash in that name would end
// the field early and leave the rest of the line as something else.
func TestATextFieldSurvivesAnAwkwardName(t *testing.T) {
	var set Set
	set.Add(
		Text(NetIP, "Wi-Fi", `he said "hi"\ and left`),
		Gauge(NetRxTotal, "Wi-Fi", 1),
	)

	out := string(set.Influx("rig", "pc", time.Unix(0, 0)))
	if strings.Contains(out, `"hi"`) {
		t.Errorf("an embedded quote was written unescaped:\n%s", out)
	}
	if !strings.Contains(out, `\"hi\"`) {
		t.Errorf("the quotes were not escaped:\n%s", out)
	}
}

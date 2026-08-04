package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Content types the pull endpoints must be served as.
const (
	PrometheusContentType = "text/plain; version=0.0.4; charset=utf-8"
	InfluxContentType     = "text/plain; charset=utf-8"
)

// DefaultMeasurement is the InfluxDB measurement name for the core group.
// Other groups append their name, e.g. rig_gpu.
const DefaultMeasurement = "rig"

// JSON renders the set as the document published to MQTT and served on the
// JSON endpoint: one field per reading, keyed the same way as the entity id.
func (s Set) JSON() map[string]any {
	out := make(map[string]any, len(s.Readings))
	for _, r := range s.Readings {
		out[r.Key()] = r.Value()
	}
	return out
}

// MarshalJSON keeps the document stable between collections by sorting keys,
// which encoding/json already does for maps.
func (s Set) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.JSON())
}

// Prometheus renders the set as a text exposition.
//
// host is attached to every sample so several machines can be scraped into
// one Prometheus without their series colliding. Text readings become info
// metrics carrying the value as a label, which is the convention for strings
// Prometheus cannot store as a sample value.
func (s Set) Prometheus(host string) []byte {
	readings := append([]Reading(nil), s.Readings...)
	sortReadings(readings)

	var b strings.Builder
	written := map[string]bool{}

	for _, r := range readings {
		if r.Def.Prom == "" {
			continue
		}
		if !written[r.Def.Prom] {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", r.Def.Prom, r.Def.Help, r.Def.Prom)
			written[r.Def.Prom] = true
		}

		labels := []string{`host="` + escapeLabelValue(host) + `"`}
		if r.Instance != "" && r.Def.InstanceLabel != "" {
			labels = append(labels, r.Def.InstanceLabel+`="`+escapeLabelValue(r.Instance)+`"`)
		}

		var sample string
		switch r.Def.Kind {
		case KindTable:
			// One series per row, which is what Prometheus labels are for. The
			// rank is a label of its own so a query can ask either "how much did
			// firefox use" or "how much did the busiest process use" — the two
			// questions a ranked list raises, and neither is answerable from a
			// series that carries only the other.
			for i, row := range r.Rows {
				rowLabels := append(append([]string(nil), labels...),
					r.Def.PromLabel+`="`+escapeLabelValue(row.Label)+`"`,
					`rank="`+strconv.Itoa(i+1)+`"`)
				fmt.Fprintf(&b, "%s{%s} %s\n", r.Def.Prom, strings.Join(rowLabels, ","),
					strconv.FormatFloat(row.Value, 'f', -1, 64))
			}
			continue
		case KindText:
			// An info metric with an empty value carries no information and
			// would create a series that never changes.
			if r.Text == "" {
				continue
			}
			labels = append(labels, r.Def.PromLabel+`="`+escapeLabelValue(r.Text)+`"`)
			sample = "1"
		case KindBool:
			sample = "0"
			if r.Bool {
				sample = "1"
			}
		default:
			sample = strconv.FormatFloat(r.Number, 'f', -1, 64)
		}

		fmt.Fprintf(&b, "%s{%s} %s\n", r.Def.Prom, strings.Join(labels, ","), sample)
	}
	return []byte(b.String())
}

// escapeLabelValue applies the escaping the Prometheus text format requires.
func escapeLabelValue(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

// Influx renders the set as InfluxDB line protocol.
//
// Each group and instance becomes its own point, so the core values land in
// "rig" while every disk gets a "rig_disk" point tagged with its drive letter.
// Text readings become tags, which is what turns "average FPS per game" into a
// GROUP BY rather than a string comparison.
func (s Set) Influx(measurement, host string, at time.Time) []byte {
	if measurement == "" {
		measurement = DefaultMeasurement
	}

	type pointKey struct {
		group    Group
		instance string
	}
	points := map[pointKey]*influxPoint{}
	var order []pointKey

	readings := append([]Reading(nil), s.Readings...)
	sortReadings(readings)

	for _, r := range readings {
		key := pointKey{group: r.Def.Group, instance: r.Instance}
		point, ok := points[key]
		if !ok {
			point = &influxPoint{}
			if r.Instance != "" && r.Def.InstanceLabel != "" {
				point.tags = append(point.tags,
					escapeTag(r.Def.InstanceLabel)+"="+escapeTag(r.Instance))
			}
			points[key] = point
			order = append(order, key)
		}

		field := escapeTag(fieldName(r))
		switch r.Def.Kind {
		case KindTable:
			// A field per rank, with the name as a string field rather than a
			// tag. A tag would be indexed, and every program that has ever led
			// the list would stay in that index forever — the textbook way to
			// ruin an InfluxDB with cardinality.
			for i, row := range r.Rows {
				rank := strconv.Itoa(i + 1)
				point.fields = append(point.fields,
					field+"_"+rank+"="+strconv.FormatFloat(row.Value, 'f', -1, 64),
					field+"_"+rank+"_"+escapeTag(r.Def.PromLabel)+`="`+escapeFieldString(row.Label)+`"`)
			}
		case KindText:
			// Empty tag values are invalid line protocol.
			if r.Text == "" {
				continue
			}
			point.tags = append(point.tags, field+"="+escapeTag(r.Text))
		case KindBool:
			point.fields = append(point.fields, field+"="+strconv.FormatBool(r.Bool))
		default:
			point.fields = append(point.fields, field+"="+strconv.FormatFloat(r.Number, 'f', -1, 64))
		}
	}

	stamp := strconv.FormatInt(at.UnixNano(), 10)
	var b strings.Builder

	for _, key := range order {
		point := points[key]
		// A point with no fields is not a valid record, which is what a group
		// consisting only of text readings would produce.
		if len(point.fields) == 0 {
			continue
		}

		name := measurement
		if key.group != GroupCore {
			name = measurement + "_" + string(key.group)
		}

		b.WriteString(escapeMeasurement(name))
		b.WriteString(",host=" + escapeTag(host))
		for _, tag := range point.tags {
			b.WriteString("," + tag)
		}
		b.WriteString(" " + strings.Join(point.fields, ","))
		b.WriteString(" " + stamp + "\n")
	}
	return []byte(b.String())
}

type influxPoint struct {
	tags   []string
	fields []string
}

// fieldName strips the group prefix from an ID, since the group is already in
// the measurement name: gpu_temperature in group gpu becomes "temperature".
func fieldName(r Reading) string {
	if r.Def.Group == GroupCore {
		return r.Def.ID
	}
	return strings.TrimPrefix(r.Def.ID, string(r.Def.Group)+"_")
}

func escapeMeasurement(s string) string {
	return strings.NewReplacer(",", `\,`, " ", `\ `).Replace(s)
}

func escapeTag(s string) string {
	return strings.NewReplacer(",", `\,`, "=", `\=`, " ", `\ `).Replace(s)
}

// escapeFieldString escapes a string field value, which unlike a tag is quoted
// and therefore only needs the quote and the backslash itself dealt with.
func escapeFieldString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// Keys lists every reading key in the set, sorted. Used by the MQTT publisher
// to work out which entities are new since the last collection.
func (s Set) Keys() []string {
	out := make([]string, 0, len(s.Readings))
	for _, r := range s.Readings {
		out = append(out, r.Key())
	}
	sort.Strings(out)
	return out
}

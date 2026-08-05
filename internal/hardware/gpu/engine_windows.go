//go:build windows

package gpu

import (
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/pdh"
)

// The WDDM engine counters: what the Task Manager's GPU tab reads.
//
// They come from the Windows graphics kernel rather than from a vendor driver,
// so they work the same on Intel, AMD and NVIDIA, need no elevation and no
// helper program. On a laptop with nothing but integrated graphics they are the
// only source of utilisation there is.
//
// Both counters expand to one row per instance, and there are a lot of rows:
// utilisation exists once per process, adapter and engine, which is several
// hundred on an ordinary machine. That is why they are sampled on a schedule of
// their own rather than on every collection.
const (
	engineCounter = `\GPU Engine(*)\Utilization Percentage`
	memoryCounter = `\GPU Adapter Memory(*)\Dedicated Usage`

	// engineInterval is how often the counters are actually collected. The
	// value is an average over the window, so a longer one is not a coarser
	// reading — it is a calmer one.
	engineInterval = 5 * time.Second
)

// luidKey identifies a graphics adapter the way both DXGI and the performance
// counters name it.
type luidKey struct {
	High int32
	Low  uint32
}

func keyOf(luid windows.LUID) luidKey {
	return luidKey{High: luid.HighPart, Low: luid.LowPart}
}

// adapterLoad is one adapter's engines, already summed over every process.
type adapterLoad struct {
	// byEngine is keyed by the engine type as Windows spells it: "3D",
	// "VideoDecode", "Copy" and so on.
	byEngine map[string]float64
	// busiest is the highest of those, which is what the Task Manager shows
	// as the utilisation of the card.
	//
	// The highest rather than the sum: three engines each at 60 % would add
	// up to 180 %, and a card cannot be more than fully busy.
	busiest float64
	// memoryBytes is dedicated video memory in use.
	memoryBytes float64
}

// engineSampler holds the two counters open and remembers the last reading.
//
// Refreshed lazily from Collect rather than from a goroutine of its own: there
// is nothing to do between samples, and a ticker would only be a lifecycle to
// get wrong.
type engineSampler struct {
	utilisation *pdh.Array
	memory      *pdh.Array

	at   time.Time
	last map[luidKey]adapterLoad
}

func newEngineSampler() *engineSampler {
	return &engineSampler{
		utilisation: pdh.NewArray(engineCounter),
		memory:      pdh.NewArray(memoryCounter),
	}
}

func (e *engineSampler) close() {
	e.utilisation.Close()
	e.memory.Close()
}

// load returns the current reading, collecting a new one when the interval has
// elapsed. A failed collection keeps the previous values rather than blanking
// the entities for one pass.
func (e *engineSampler) load() map[luidKey]adapterLoad {
	if time.Since(e.at) < engineInterval {
		return e.last
	}

	values, err := e.utilisation.Values()
	if err != nil {
		return e.last
	}
	e.at = time.Now()

	loads := map[luidKey]adapterLoad{}
	for instance, value := range values {
		key, engine, ok := parseEngineInstance(instance)
		if !ok || value <= 0 {
			continue
		}
		load, seen := loads[key]
		if !seen {
			load.byEngine = map[string]float64{}
		}
		load.byEngine[engine] += value
		if sum := load.byEngine[engine]; sum > load.busiest {
			load.busiest = sum
		}
		loads[key] = load
	}

	// The memory counter is a second query and may fail on its own; the
	// utilisation figures do not depend on it.
	if used, err := e.memory.Values(); err == nil {
		for instance, value := range used {
			key, ok := parseLUID(instance)
			if !ok {
				continue
			}
			load := loads[key]
			load.memoryBytes += value
			loads[key] = load
		}
	}

	e.last = loads
	return loads
}

// parseEngineInstance pulls the adapter and the engine type out of an instance
// name such as
//
//	pid_20876_luid_0x00000000_0x00017F59_phys_0_eng_5_engtype_VideoEncode
func parseEngineInstance(instance string) (luidKey, string, bool) {
	key, ok := parseLUID(instance)
	if !ok {
		return key, "", false
	}
	const marker = "_engtype_"
	idx := strings.Index(instance, marker)
	if idx < 0 {
		return key, "", false
	}
	engine := instance[idx+len(marker):]
	if engine == "" {
		return key, "", false
	}
	return key, engine, true
}

// parseLUID reads the adapter identifier out of an instance name.
//
// Parsed into numbers rather than compared as text: the counter spells the LUID
// as two hexadecimal words, and matching that spelling against a formatted
// version of what DXGI reports would make the whole mapping depend on getting
// the padding right.
func parseLUID(instance string) (luidKey, bool) {
	const marker = "luid_"
	idx := strings.Index(instance, marker)
	if idx < 0 {
		return luidKey{}, false
	}
	rest := instance[idx+len(marker):]

	high, rest, ok := parseHexWord(rest)
	if !ok {
		return luidKey{}, false
	}
	low, _, ok := parseHexWord(rest)
	if !ok {
		return luidKey{}, false
	}
	// The high half is signed in the Windows structure and effectively always
	// nought; the counter writes it unsigned either way.
	return luidKey{High: int32(uint32(high)), Low: uint32(low)}, true
}

// parseHexWord reads a leading "0xABCD_" and returns the value and what
// follows it.
func parseHexWord(s string) (uint64, string, bool) {
	if !strings.HasPrefix(s, "0x") {
		return 0, s, false
	}
	s = s[2:]
	end := strings.IndexByte(s, '_')
	if end < 0 {
		end = len(s)
	}
	value, err := strconv.ParseUint(s[:end], 16, 32)
	if err != nil {
		return 0, s, false
	}
	if end < len(s) {
		return value, s[end+1:], true
	}
	return value, "", true
}

// engineDefinitions maps the engine types worth reporting onto their
// measurements.
//
// Deliberately not every engine Windows lists. VR, OFA, Security, JPEG decode
// and the legacy overlay sit at nought on ordinary hardware and would be five
// entities that never say anything.
var engineDefinitions = []struct {
	engine string
	def    metrics.Definition
}{
	{"3D", metrics.GPUEngine3D},
	{"VideoDecode", metrics.GPUEngineDecode},
	{"VideoEncode", metrics.GPUEngineEncode},
	{"Copy", metrics.GPUEngineCopy},
}

// mergeEngines adds the counter readings for every adapter DXGI named.
//
// Runs after the vendor sources, because the overall utilisation is only a
// fallback: NVML and Afterburner measure the same thing closer to the hardware,
// and two sources writing one measurement would put it in the set twice.
func (e *engineSampler) merge(set *metrics.Set, adapters map[string]windows.LUID) bool {
	if len(adapters) == 0 {
		return false
	}
	loads := e.load()
	if len(loads) == 0 {
		return false
	}

	previous := set.Origin
	set.Origin = "Windows"
	defer func() { set.Origin = previous }()

	added := false
	for instance, luid := range adapters {
		load, ok := loads[keyOf(luid)]
		if !ok {
			continue
		}

		for _, entry := range engineDefinitions {
			value, reported := load.byEngine[entry.engine]
			if !reported {
				continue
			}
			set.Add(metrics.Gauge(entry.def, instance, clampPercent(value)))
			added = true
		}
		if load.memoryBytes > 0 {
			set.Add(metrics.Gauge(metrics.GPUMemoryUsed, instance, load.memoryBytes/(1024*1024)))
			added = true
		}

		// The fallback. On a machine with NVML or Afterburner this never
		// fires; on a laptop with integrated graphics it is the only
		// utilisation there is.
		if load.busiest > 0 {
			if _, exists := set.Find(metrics.GPULoad.ID, instance); !exists {
				set.Add(metrics.Gauge(metrics.GPULoad, instance, clampPercent(load.busiest)))
				added = true
			}
		}
	}
	return added
}

// clampPercent keeps a summed counter inside the range it claims to be in.
// Adding several processes' shares of one engine can land a little over a
// hundred when a sample straddles a scheduling boundary.
func clampPercent(value float64) float64 {
	if value > 100 {
		return 100
	}
	if value < 0 {
		return 0
	}
	return value
}

# Adding your own measurements

!!! tip "Only after the data that is already there?"

    Then you do not need this page. How to fetch the values over JSON,
    Prometheus, line protocol or MQTT is described under
    [Export targets](export-targets.md) — nothing has to be programmed for that.

This page describes the other case: you want to report a value the program does
not know yet. A sensor on the mainboard, a value out of an application of your
own, a number from a file.

## Why this is little work

There is **no** export-specific code to write. A measurement is described in
exactly one place, and everything else follows from that by itself: MQTT
discovery in Home Assistant, the JSON at `/api/state`, the Prometheus exposition
at `/metrics` and the line protocol for InfluxDB. The exporters know no single
measured quantity, they know only the catalogue.

That is also the promise you must not break while doing it: **the output format
is identical across all data sources.** Where a value came from reaches no
export.

## The four steps

### 1. Describe the measurement

Create a `Definition` in `internal/metrics/definitions.go` and enter it in `All`
at the bottom — the order there is the catalogue order and thereby also the
display order.

```go
var MainboardTemperature = Definition{
    ID:   "mainboard_temperature",
    Name: i18n.Text{DE: "Mainboard-Temperatur", EN: "Mainboard temperature"},
    Unit: "°C", Kind: KindGauge, Precision: 1, Group: GroupCPU,
    Prom: "rig_mainboard_temperature_celsius",
    Help: "Mainboard temperature reported by the embedded controller",
    DeviceClass: "temperature", StateClass: "measurement",
    Icon: "mdi:thermometer",
}
```

The fields you can get wrong:

| Field | Rule |
|---|---|
| `ID` | Short, lower case, **never translated**. Entity ids, dashboards and automations hang off it |
| `Name` | Exactly the other way round: always follows the language |
| `Kind` | `KindGauge`, `KindText`, `KindBool` or `KindTable` — the last of these a short ranking with one name and one number per row. Decides whether a `sensor` or a `binary_sensor` comes out of it |
| `Group` | Decides **which switch turns the value off** — `GroupGPU`, `GroupCPU`, `GroupRAM`, `GroupDisk`, `GroupNet`, `GroupCooling`, `GroupBattery`. `GroupCore` is always collected and has no switch; such values can only be deselected in the measurement selection |
| `Panel` | Only needed when the value is displayed somewhere other than where it is collected |
| `InstanceLabel` | Set it as soon as the value exists more than once (`"gpu"`, `"disk"`, `"nic"`, `"core"`) |
| `EntityCategory` | `diagnostic` for facts *about* the machine, empty for measurements *on* it |
| `NoEntity` | Keeps the value out of Home Assistant without taking it out of JSON, Prometheus and InfluxDB |

`Prom` and `Help` stay **English**. That is a machine-readable format, read by a
scraper and by whoever writes the query.

### 2. Sort it into the scopes

`internal/metrics/sets.go`. **Extended** is the whole catalogue and needs
nothing — the new value is in it automatically. Whoever wants it in **Minimal**
or **Standard** as well enters its ID in `minimalSet` or `basicSet`
respectively.

The rungs are deliberately maintained lists and not a rule over the definitions,
because no rule gets it right: `diagnostic` describes where Home Assistant files
an entity, not whether anybody wants to see it. They nest inside one another —
Minimal ⊂ Standard ⊂ Extended — and a test holds that fast.

### 3. Write the source

A source is anything that has two methods:

```go
type Source interface {
    Group() metrics.Group
    Collect(set *metrics.Set) error
}
```

`Collect` **appends to** the set instead of returning a list. That is
deliberate: a source that gets halfway through — three of four disks readable —
reports what it has all the same. An error is a diagnosis, not a reason to throw
the measurements away.

```go
func (s *Mainboard) Group() metrics.Group { return metrics.GroupCPU }

func (s *Mainboard) Collect(set *metrics.Set) error {
    celsius, err := s.read()
    if err != nil {
        return err          // no set.Add — the value is then simply missing
    }
    set.Add(metrics.Gauge(metrics.MainboardTemperature, "", celsius))
    return nil
}
```

The second parameter of `Gauge` is the instance and stays empty as long as the
value exists only once. For text, booleans and rankings there are
`metrics.Text`, `metrics.Bool` and `metrics.Table`.

Three rules somebody otherwise burns their fingers on:

!!! warning "Leave missing values out, do not zero them"

    A 0 claims it is zero degrees. A missing field claims nothing. When in
    doubt, add nothing at all.

!!! warning "The clock is running"

    Every source gets **half a read interval** — at the preset 500 ms that is
    250 ms. Whoever needs longer is cut off and reports "no answer". A slow
    source therefore belongs on a goroutine of its own that writes into a cache;
    `Collect` then only reads that cache. The cooling source does exactly that.

!!! warning "A panic silences your source"

    If a source panics, the collector catches it, writes it into `SourceErrors`
    and keeps measuring with the remaining sources — so it does not take the
    program down with it. Your source, however, is switched off and stays that
    way: every following tick returns immediately with the same error. It only
    starts up again when the sensors are rebuilt, that is at program start or
    after a change to the sensor settings. A single panic therefore costs not
    one measurement but the source. Better to catch the edge cases yourself.

If **several** sources supply the same value, the cheapest one wins. The
expensive one goes to the back in `Collect` and writes only what the cheap one
did not supply — the way `gpu_load` from the WDDM counters only steps in when
Afterburner and NVML stay silent. Two exceptions: when the more expensive source
measures *more precisely*, and when two sources only seemingly supply the same
thing — then they are two measurements with identifiers of their own.

### 4. Register it

In `internal/app/sources.go`, `buildSensors` assembles the optional sources, and
`buildCollector` in `internal/app/app.go` passes them on:

```go
c.AddSource(s.sources...)
```

Your source into this list — done. For hardware that does not exist on this
machine, do not attach the source in the first place; in `buildSensors` every
attachment sits behind an `if`. `AddSource` does skip a `nil`, but that only
applies to the empty interface: a constructor with a concrete return type —
`func New() *Mainboard` — delivers an interface that is not `nil` even with
`return nil`, and runs into a panic on the first `Collect`.

## The contract that will stop you

On the first `go test ./...` a test fails, and that is as it should be:

```
go test ./internal/metrics/ -update-catalogue
```

`internal/metrics/testdata/catalogue.txt` holds the identifier, unit, kind,
precision, group, panel, category and Prometheus name of every measurement. If
any of that changes, the test breaks. The command rewrites the file — it belongs
in the commit, and the commit belongs to say which consumers have to be moved
over.

**Adding a line is the clean extension. Renaming orphans entities**, because a
discovery message is *retained*: it sits on the broker, survives the program and
survives deleting the entity by hand as well. Cleaning up therefore always
means: empty the broker first, then Home Assistant.

## Checking

```powershell
.\build.ps1 -Check
```

And afterwards with your eyes: `-probe` shows in the console which program
supplied how many values, and the **Data sources** panel on the Dashboard page
shows the same. Both lists arise out of the values that are there — a row with
zero values therefore does not exist: your source only turns up when it has
supplied at least one value. And under a name of its own only when it implements
`collector.OriginNamer`; otherwise it counts under "Windows". That a source runs
and finds nothing you read off the empty group and off the error it leaves
behind in `SourceErrors`.

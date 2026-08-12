# Export targets

**Four ways, and the choice is yours: one, several or all of them at once.** They
do not exclude each other and know nothing of each other — every target can be
switched on and off on its own, without the others being touched.

| Target | Direction | Default |
|---|---|---|
| MQTT | push — the program sends | **on** |
| HTTP data server (JSON, Prometheus, line protocol) | pull — the receiver fetches | off |
| Prometheus | pull, through the data server | on as soon as the data server runs |
| InfluxDB | push — the program writes | off |

The measuring happens only once anyway. Switching on a second target therefore
costs almost nothing: the same snapshot is simply written out a second time. And
it looks the same everywhere — which source supplied a value reaches no export.

A common setup is MQTT for Home Assistant *and* Prometheus for the long-term
charts in Grafana, because Home Assistant is the wrong tool for weeks of
two-second readings.

All of this is switched on and off on
[Export & display](interface/export-and-display.md); this page says what comes out
of it and how a receiver reads it.

## MQTT

On by default; the credentials are under
[Export & display → MQTT](interface/export-and-display.md#mqtt-push-to-home-assistant).
A device appears in Home Assistant whose entities say where they come from:

```
sensor.re_corganpc2_fps            sensor.re_corganpc2_gpu0_temperature
sensor.re_corganpc2_cpu            sensor.re_corganpc2_diskc_used_percent
sensor.re_corganpc2_game           sensor.re_corganpc2_net_ethernet_2_rx
sensor.re_corganpc2_ping_rtt       binary_sensor.re_corganpc2_rtss
```

For how they are put together see
[How the identifiers are built](identifiers.md#how-the-identifiers-are-built).

The id is requested through `default_entity_id`. `object_id` used to do that and
was removed from the MQTT component in Home Assistant 2026 — both are sent, so
that older versions are still served. Without either of them Home Assistant
assembles the id from the device name and the entity name, and
`re_corganpc2_diskc_busy` becomes `corganpc2_busy_c`.

Topics — `<node>` and the two prefixes are set under
[Export & display → Home Assistant](interface/export-and-display.md#home-assistant):

```
homeassistant/sensor/rig_<node>/<key>/config   discovery, retained
rig-exporter/<node>/state                      one JSON for all entities
rig-exporter/<node>/availability               online/offline, last will
```

Discovery follows what was actually measured: start Afterburner later and the
GPU entities are there at the next interval — without a restart. If hardware
disappears again, the entities stay, so that history and dashboards do not
break. If the process dies, the broker sets everything to `unavailable` through
the last will.

**The "Visit" link** on the device page in Home Assistant points at this
exporter's interface — and at the port it is *actually* listening on. If the
configured port is taken, the web server falls back to a random one; the
discovery message is then rewritten with the correct address as soon as it is
known. That is necessary because discovery is retained: a message once published
with the wrong port would otherwise stay wrong until somebody overwrote it.

Which address it carries depends on what the server listens on. By default that
is loopback only, so `http://127.0.0.1:<port>` — the link then works when Home
Assistant is open in a browser **on this PC**, not from a phone. With
[**Make this page reachable on the network**](interface/export-and-display.md#application)
set, it carries this machine's LAN address and the link works from anywhere. See
[The interface on the network](interface/on-the-network.md).

### Game attributes

With [working out the game](interface/data-capture.md#working-out-the-game)
switched on, the state document carries one more key, `game_details`, and the
**game** entity is discovered with `json_attributes_topic` pointing at that same
state topic. One message, no second entity, and the entity's own state stays
what it always was — the executable RTSS reported:

```json
{ "game": "Cyberpunk2077.exe",
  "game_details": { "platform": "gog", "title": "Cyberpunk 2077", "app_id": "1091500" } }
```

In Home Assistant that reads as `state_attr('sensor.re_corganpc2_game',
'app_id')`, which is what addresses the artwork:

```
https://cdn.cloudflare.steamstatic.com/steam/apps/{{ app_id }}/header.jpg
```

**Each of the three is present only when it is known.** A game the store has
never heard of keeps its platform and title and has no `app_id`; an executable
no launcher claims produces no `game_details` at all, and the attributes are
then empty rather than stale — the same message clears what the last game left
behind.

In Prometheus the same three arrive as one info metric,
`rig_game_details_info{platform="gog",title="Cyberpunk 2077",app_id="1091500"} 1`,
and in InfluxDB as the string fields `game_details_platform`,
`game_details_title` and `game_details_app_id` on the `rig` point.

## HTTP data server

A second listener, `0.0.0.0:9838` by default, so that Home Assistant can reach
it from another machine. It is switched on under
[Export & display → Data server](interface/export-and-display.md#data-server-home-assistant-and-prometheus-pull).

The three data formats deliver the same readings — the choice depends only on
who fetches them:

| Path | Format | For whom | Peculiarity |
|---|---|---|---|
| `/api/state` | **JSON**, one object with every value, identical to the MQTT state | RESTful sensor in Home Assistant, your own scripts | Easiest to read, but in Home Assistant **every single value** needs its own sensor entry with a template |
| `/metrics` | **Prometheus** text exposition, one line per time series | Prometheus, Grafana Agent, VictoriaMetrics | Brings type and help text with it; values that exist more than once appear as labels (`gpu="0"`) instead of as names of their own |
| `/influx` | **InfluxDB line protocol** | Telegraf and anything that reads line protocol | The same content as the push further down, only fetched instead of sent |
| `/health` | `ok` as text, otherwise 503 | uptime watchdogs, container health checks | Answers **always without a token** |
| `/` | overview of the active endpoints | a person in a browser | Needs a token, like the data |

An optional token, checked as `Authorization: Bearer <token>` or `?token=`.
Without a token anyone on the network can read the values.

With a token set, the port stays silent for the overview page as well: it names
the version and the node id, and those are exactly the details that tell a
stranger whether a second look is worth it. Only `/health` keeps answering
without a token — a liveness check that needs a secret checks nothing.

While the server runs, the finished addresses stand as clickable links on the
Dashboard and in the settings block. They carry the IP address of the interface
the default route runs over, not the machine name — the IP works even where name
resolution on the local network does not.

Home Assistant, `configuration.yaml`:

```yaml
rest:
  - resource: http://corganpc2:9838/api/state
    headers:
      Authorization: !secret rig_exporter_token   # only when a token is set
    scan_interval: 5
    sensor:
      - name: FPS CorganPC2
        unique_id: fps_corganpc2_rest
        value_template: "{{ value_json.fps }}"
        unit_of_measurement: fps
        state_class: measurement
      - name: GPU CorganPC2
        unique_id: gpu_temp_corganpc2_rest
        value_template: "{{ value_json.gpu0_temperature }}"
        unit_of_measurement: "°C"
        device_class: temperature
        state_class: measurement
      - name: SSD C CorganPC2
        unique_id: disk_c_corganpc2_rest
        value_template: "{{ value_json.diskc_used_percent }}"
        unit_of_measurement: "%"
        state_class: measurement
```

## Prometheus

Part of the data server: it is switched on in the same box, under
*Prometheus exporter at `/metrics`* — see
[Export & display → Data server](interface/export-and-display.md#data-server-home-assistant-and-prometheus-pull).

```yaml
scrape_configs:
  - job_name: rig-exporter
    scrape_interval: 5s
    static_configs:
      - targets: ["corganpc2:9838"]
    authorization:          # only when a token is set
      type: Bearer
      credentials: <token>
```

Every series carries `host="<node_id>"`, so several PCs do not collide.
Instances become labels: `rig_disk_used_percent{host="corganpc2",disk="C:"}`,
`rig_gpu_temperature_celsius{host="corganpc2",gpu="0"}`,
`rig_net_receive_megabits_per_second{host="corganpc2",nic="Ethernet"}`.
Text values become info metrics: `rig_game_info{game="Cyberpunk2077.exe"} 1`.
Which reading gets which name is under
[Ids and where they land](identifiers.md).

## InfluxDB

Two ways, switchable independently.

**Pull** — `/influx` delivers line protocol, for Telegraf say:

```toml
[[inputs.http]]
  urls = ["http://corganpc2:9838/influx"]
  data_format = "influx"
  headers = { Authorization = "Bearer <token>" }
```

**Push** — rig-exporter writes to the InfluxDB v2 write API (`/api/v2/write`)
itself. The fields for it — URL, bucket, organisation and token, and how they
are filled differently for InfluxDB 1.8 — are under
[Export & display → InfluxDB](interface/export-and-display.md#influxdb-push).

Every group becomes a measurement of its own, every instance a point of its own:

```
rig,host=corganpc2 fps=143.2,cpu=24.5,ram=51.3,game="Cyberpunk2077.exe",resolution="2560x1440" …
rig_gpu,host=corganpc2,gpu=0 temperature=61,core_clock=2730,vram_used=5750,name="RTX 4090" …
rig_disk,host=corganpc2,disk=C: used_percent=77,read=0.4,write=3.6,media="NVMe" …
rig_net,host=corganpc2,nic=Ethernet rx=3.4,tx=0.13,ip="192.168.1.42" …
```

**A tag is only what names the measured thing:** the machine and the instance —
drive, adapter, graphics card, memory bank. "Write load per disk" is therefore a
`GROUP BY disk`.

**Everything else is a field, even when it is text** — game name, resolution, IP
address, driver version. In InfluxDB the set of tags *is* the identity of the
series, and a text that changes while running would drag it along: after an IP
change, `rx_total` and `tx_total` of the same adapter would carry on in a second
series, with sums of their own starting from zero. A game name as a tag would
additionally be in the index forever — every game that ever ran.

For an evaluation "per game" that means a field comparison instead of a
`GROUP BY`. That is the price, and it is paid on purpose: the alternative is
torn counter series and an index that grows with every game.

---

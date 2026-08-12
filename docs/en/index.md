<div class="hero" markdown>

# ![](images/rig-exporter-mark.svg) rig-exporter

Telemetry of a gaming PC for Home Assistant, Prometheus and InfluxDB.
A single Windows program, around 18 MB, without installation and without
dependencies.

[Get started in five minutes](getting-started/index.md){ .md-button .md-button--primary }
[On GitHub](https://github.com/corgan2222/rig-exporter){ .md-button }

</div>

It reads what the machine knows about itself — frames per second,
temperatures, load, free space, throughput, battery — and sends it where you
want to see it.

![The Dashboard page of rig-exporter](images/screenshots/en/dashboard.png)

<div class="grid cards" markdown>

-   :material-home-assistant: **Home Assistant without handiwork**

    MQTT discovery creates the entities itself, with device, icons and units.
    Nothing to enter in `configuration.yaml`.

-   :material-chart-line: **Prometheus and InfluxDB**

    The same values as a text exposition and as line protocol — to be fetched
    or written out on their own.

-   :material-tune: **You decide what is sent**

    122 measurements in the catalogue, each one can be deselected separately.
    Three presets from "the tiles only" to "everything".

-   :material-eye-off: **Only what is really there**

    No battery, no battery readings. A missing value is left out, not invented
    as a zero.

</div>

## In five minutes

1. Download the `rig-exporter.exe` from the [release](https://github.com/corgan2222/rig-exporter/releases)
   and start it. The interface opens at
   `http://127.0.0.1:8787`.
2. Under [**Export & display → MQTT**](interface/export-and-display.md#mqtt-push-to-home-assistant)
   enter the MQTT broker.
3. Done. Home Assistant shows the device after a few seconds.

Everything beyond that — which measurements exist, what you have to have
installed for them, how the values come about — is in this handbook.

## Where to start

- [What is reported](what-is-reported.md) — the full measurement catalogue,
  sorted by group
- [Requirements](requirements.md) — what works without an extra program and
  what does not
- [First start](first-run.md) — autostart, configuration file, language
- [Interface](interface/index.md) — the four pages and what is on them

!!! info "Windows only"

    rig-exporter reads interfaces that exist only under Windows: DXGI, the
    WDDM performance counters, RTSS' shared memory. A Linux edition is not
    planned, because none of it exists there.

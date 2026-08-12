# Home Assistant

What to do with the entities once they are there. How they come into being in
the first place is under [Export targets → MQTT](export-targets.md#mqtt) — this
page is only about displaying them.

## HACS integrations

[HACS](https://hacs.xyz/) — the Home Assistant Community Store — is the usual
way to install cards and integrations that are not shipped with Home Assistant.

**For the normal case you do not need it.** The device, its entities, the
histories and the “Visit” link all come from MQTT discovery, and everything
built on top of that can be done with the built-in cards. Anyone who has not
installed HACS loses nothing fundamental here.

There is one exception:

| Integration | What for | Needed? |
|---|---|---|
| [**ApexCharts Card**](https://github.com/RomRider/apexcharts-card) | The [column chart of the top processes](#column-chart-over-time) over time — several series out of *one* attribute, which no built-in card can do | only for that |

Everything else on this page works without it.

## Cards

Which built-in card fits which question:

| You want to see … | Card | Good for |
|---|---|---|
| one value, large and readable at a glance | **Gauge** | temperature, load, usage — anything with a natural upper limit |
| many values below one another | **Entities** | one panel per piece of hardware: everything about the GPU, everything about the disk |
| one value, compact, with an icon | **Tile** | dashboard tiles, FPS and temperature side by side |
| the history of the last few hours | **History Graph** | temperature and load across a gaming session |
| daily or weekly values | **Statistics Graph** | long-term trends, far cheaper than the full history |
| something you have put together yourself | **Markdown** | lists built from attributes, like the top processes below |

Two notes that save time:

**Diagnostic entities do not turn up by themselves.** Everything filed as
`diagnostic` — model, capacity, rated values, Windows version — is kept out of
automatically generated dashboards by Home Assistant. On the device page it sits
under *Diagnostic*, and in a card it can be added by hand like anything else.
Which value ends up where is under
[Identifiers and categories](identifiers.md#where-home-assistant-files-the-values).

**For long-term charts Home Assistant is the wrong tool.** At a send interval of
two seconds the database grows quickly; anyone who wants to look back over weeks
is better served by [Prometheus](export-targets.md#prometheus) and Grafana. Under
[*Export & display → Long-term storage*](interface/export-and-display.md#long-term-storage-in-home-assistant)
the interface hands out a ready-made `recorder:` block for exactly this machine.

## Card configuration

Two worked examples for the
[top processes](diagnostics.md#top-processes) — the list sits as an attribute on
*one* entity, and that makes displaying it less obvious than for an ordinary
measurement.

The same shape of problem turns up with the artwork of the running game, which
is addressed through an attribute of that same entity; that card is under
[Game identification](game-identification.md#on-a-home-assistant-dashboard).

### The current list, without an extra card

A Markdown card is enough and needs no HACS. The numbering is what ties it to
the legend of the chart:

```yaml
type: markdown
entity_id:
  - sensor.re_corganpc2_top_cpu
content: |
  {% set apps = state_attr('sensor.re_corganpc2_top_cpu','apps') %}
  {% if apps %}{% for app in apps %}**{{ loop.index }}.** {{ app.name }} — **{{ app.value }} %**

  {% endfor %}{% else %}_no reading yet_{% endif %}
```

### Column chart over time

This one needs **ApexCharts Card** from HACS. The complete card; for memory the
same with `sensor.re_corganpc2_top_memory`:

```yaml
type: custom:apexcharts-card
header:
  show: true
  title: CPU share of the five largest programs
graph_span: 6h
stacked: true
all_series_config:
  type: column
  extend_to: false
  group_by:
    func: avg
    duration: 5min
series:
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank1, name: Rank 1}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank2, name: Rank 2}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank3, name: Rank 3}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank4, name: Rank 4}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank5, name: Rank 5}
apex_config:
  legend:
    formatter: >-
      EVAL:function (s) { try { var i = String(s).split(' ').pop();
      var a = document.querySelector('home-assistant').hass
        .states['sensor.re_corganpc2_top_cpu'].attributes;
      var n = a['rank' + i + '_name']; return n ? n : s; }
      catch (e) { return s; } }
  tooltip:
    y:
      title:
        formatter: >-
          EVAL:function (s) { try { var i = String(s).split(' ').pop();
          var a = document.querySelector('home-assistant').hass
            .states['sensor.re_corganpc2_top_cpu'].attributes;
          var n = a['rank' + i + '_name']; return n ? n : s; }
          catch (e) { return s; } }
```

Four things in it are not optional:

**`attribute:` with a flat rank.** That is the whole mechanism — the card reads
`attributes['rank1']` out of **every** entry in the history. A list of objects
is something it cannot draw.

**`group_by`.** At a sampling interval of 10 s, 6 hours hold 2160 points per
series, times five series more than 10 000 columns. Without bundling the card
paints strokes. `avg` and not `max`, because stacked maxima add up that never
occurred at the same time — the stack would then stand above 100 %.

**Legend *and* tooltip.** The two run through different formatters, and the card
itself only sets `tooltip.y.formatter`, for the *value*. The title in front of
it otherwise stays “Rank 1”. That your own `title.formatter` does not displace
the value formatter — unit and rounding stay — is because `apex_config` is taken
over by deep merge.

**The `try`/`catch`.** Without it, an error in the formatter takes the whole card
with it; with it, the worst case is “Rank 1” standing there again.

The rank is read out of the series name (`"Rank 3"` → `3`), because the tooltip
formatter only gets the name and no index. Anyone who renames the series has to
rebuild the function along with them.

> **Do not use `data_generator`.** It suggests itself, because `apps` fits so
> nicely — the option “completely bypasses the history retrieval” and sees only
> the *current* state. It can therefore never produce more than one point, and
> the card loads endlessly instead of showing an error. `data_generator` is
> meant for attributes that already contain a time series themselves, a weather
> forecast for instance.

Two things one has to know while reading this: both formatters reach into the
internals of the frontend through `document.querySelector('home-assistant')` and
can break with a Home Assistant update. And the name shown is always the
**current** one — above a column from four hours ago stands whoever is at rank 2
*now*, not whoever was back then. Anyone who does not want that leaves out
the `apex_config` and puts the Markdown card from above underneath it.

All of this can be checked in the browser console without aiming at a tooltip —
synthetic mouse events do not trigger one in ApexCharts anyway:

```js
// walk through the shadow roots to <apexcharts-card>, then:
const chart = card[Object.keys(card).find(k => k.toLowerCase().includes('apexchart'))];
chart.w.config.tooltip.y.title.formatter("Rank 1");   // -> "firefox.exe"
typeof chart.w.config.tooltip.y.formatter === 'function'; // -> true, unit intact
```

In the other export formats the question does not arise: Prometheus gets one
series per row with an `app` and a `rank` label, InfluxDB one field per rank.

```
rig_top_cpu_percent{host="corganpc2",app="Cyberpunk2077.exe",rank="1"} 62.43
rig_top_cpu_percent{host="corganpc2",app="firefox.exe",rank="2"} 4.15
```

Anyone who needs these values as a long-term chart and wants to avoid HACS is far
better served by [Prometheus](export-targets.md#prometheus) and Grafana than by Home
Assistant.

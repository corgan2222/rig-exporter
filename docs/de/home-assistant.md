# Home Assistant

Was Sie mit den Entities anfangen, sobald sie da sind. Wie sie überhaupt
entstehen, steht unter [Exportziele → MQTT](export-targets.md#mqtt) — hier geht es
nur um die Anzeige.

## HACS Integrationen

[HACS](https://hacs.xyz/) — der Home Assistant Community Store — ist der übliche
Weg, Karten und Integrationen zu installieren, die nicht mitgeliefert werden.

**Für den Normalfall brauchen Sie ihn nicht.** Das Gerät, seine Entities, die
Verläufe und der „Visit"-Link entstehen über MQTT-Discovery, und alles, was
darauf aufsetzt, lässt sich mit den mitgelieferten Karten bauen. Wer HACS nicht
installiert hat, verliert hier nichts Grundsätzliches.

Eine Ausnahme gibt es:

| Integration | Wofür | Nötig? |
|---|---|---|
| [**ApexCharts Card**](https://github.com/RomRider/apexcharts-card) | Das [Säulendiagramm der Top-Prozesse](#saulendiagramm-uber-die-zeit) über die Zeit — mehrere Serien aus *einem* Attribut, das kann keine mitgelieferte Karte | nur dafür |

Alles Übrige auf dieser Seite kommt ohne aus.

## Karten

Welche mitgelieferte Karte zu welcher Frage passt:

| Sie wollen sehen … | Karte | Gut geeignet für |
|---|---|---|
| einen Wert, groß und sofort lesbar | **Gauge** | Temperatur, Auslastung, Belegung — alles mit einer natürlichen Obergrenze |
| viele Werte untereinander | **Entities** | ein Panel je Hardware: alles zur GPU, alles zur Platte |
| einen Wert kompakt mit Symbol | **Tile** | Dashboard-Kacheln, FPS und Temperatur nebeneinander |
| den Verlauf der letzten Stunden | **History Graph** | Temperatur und Auslastung über eine Spielsitzung |
| Tages- oder Wochenwerte | **Statistics Graph** | langfristige Trends, deutlich sparsamer als der volle Verlauf |
| etwas selbst Zusammengesetztes | **Markdown** | Listen aus Attributen, so wie unten die Top-Prozesse |

Zwei Hinweise, die Zeit sparen:

**Diagnose-Entities tauchen nicht von selbst auf.** Alles, was als
`diagnostic` eingeordnet ist — Modell, Kapazität, Nennwerte, Windows-Version —
hält Home Assistant aus automatisch erzeugten Dashboards heraus. Auf der
Geräteseite steht es unter *Diagnose*, und in einer Karte lässt es sich ganz
normal von Hand hinzufügen. Welcher Wert wo landet, steht unter
[Bezeichner und Einordnung](identifiers.md#wo-home-assistant-die-werte-einsortiert).

**Für Langzeitdiagramme ist Home Assistant das falsche Werkzeug.** Bei einem
Sendeintervall von zwei Sekunden wächst die Datenbank schnell; wer Wochen
zurückblicken will, ist mit [Prometheus](export-targets.md#prometheus) und Grafana
besser bedient. Die Oberfläche gibt unter
[*Export & Anzeige → Langzeitspeicherung*](interface/export-and-display.md#langzeitspeicherung-in-home-assistant)
einen fertigen `recorder:`-Block für genau diesen Rechner aus.

## Karten-Konfiguration

Zwei ausgearbeitete Beispiele für die
[Top-Prozesse](diagnostics.md#top-prozesse) — die Liste liegt als Attribut an
*einer* Entity, und das macht die Anzeige weniger offensichtlich als bei einem
gewöhnlichen Messwert.

### Die aktuelle Liste, ohne Zusatzkarte

Eine Markdown-Karte reicht und braucht kein HACS. Die Nummerierung ist das, was
sie mit der Legende des Diagramms verbindet:

```yaml
type: markdown
entity_id:
  - sensor.re_corganpc2_top_cpu
content: |
  {% set apps = state_attr('sensor.re_corganpc2_top_cpu','apps') %}
  {% if apps %}{% for app in apps %}**{{ loop.index }}.** {{ app.name }} — **{{ app.value }} %**

  {% endfor %}{% else %}_noch keine Messung_{% endif %}
```

### Säulendiagramm über die Zeit

Dafür braucht es **ApexCharts Card** aus HACS. Die vollständige Karte, für den
Speicher genauso mit `sensor.re_corganpc2_top_memory`:

```yaml
type: custom:apexcharts-card
header:
  show: true
  title: CPU-Anteil der fünf größten Programme
graph_span: 6h
stacked: true
all_series_config:
  type: column
  extend_to: false
  group_by:
    func: avg
    duration: 5min
series:
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank1, name: Platz 1}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank2, name: Platz 2}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank3, name: Platz 3}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank4, name: Platz 4}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank5, name: Platz 5}
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

Vier Stellen daran sind nicht optional:

**`attribute:` mit einem flachen Rang.** Das ist der ganze Mechanismus — die
Karte liest `attributes['rank1']` aus **jedem** Eintrag der History. Eine Liste
von Objekten kann sie nicht zeichnen.

**`group_by`.** Bei 10 s Messintervall liegen in 6 Stunden 2160 Punkte je Serie,
mal fünf Serien über 10 000 Säulen. Ohne Bündelung malt die Karte Striche.
`avg` und nicht `max`, weil sich gestapelte Maxima addieren, die nie gleichzeitig
auftraten — der Stapel stünde dann über 100 %.

**Legende *und* Tooltip.** Die beiden laufen durch verschiedene Formatter, und
die Karte setzt selbst nur `tooltip.y.formatter` für den *Wert*. Der Titel davor
bleibt sonst „Platz 1". Dass der eigene `title.formatter` den Wert-Formatter
nicht verdrängt — Einheit und Rundung bleiben —, liegt daran, dass `apex_config`
per Deep Merge übernommen wird.

**Das `try`/`catch`.** Ohne es nimmt ein Fehler im Formatter die ganze Karte mit;
mit ihm steht im schlimmsten Fall wieder „Platz 1" da.

Der Platz wird aus dem Seriennamen gelesen (`"Platz 3"` → `3`), weil der
Tooltip-Formatter nur den Namen bekommt und keinen Index. Wer die Serien
umbenennt, muss die Funktion mit umbauen.

> **Nicht `data_generator` benutzen.** Das liegt nahe, weil `apps` so schön
> passt — die Option „completely bypasses the history retrieval" und sieht nur
> den *aktuellen* Zustand. Sie kann damit nie mehr als einen Punkt erzeugen,
> und die Karte lädt endlos, statt einen Fehler zu zeigen. `data_generator` ist
> für Attribute gedacht, die selbst schon eine Zeitreihe enthalten, etwa eine
> Wettervorhersage.

Zwei Dinge, die man beim Lesen wissen muss: die beiden Formatter greifen über
`document.querySelector('home-assistant')` in die Interna des Frontends und
können mit einem Home-Assistant-Update brechen. Und der angezeigte Name ist
immer der **aktuelle** — über einer Säule von vor vier Stunden steht, wer
*jetzt* auf Platz 2 ist, nicht wer es damals war. Wer das nicht will, lässt die
`apex_config` weg und stellt die Markdown-Karte von oben darunter.

Prüfen lässt sich das Ganze in der Browser-Konsole, ohne auf einen Tooltip zu
zielen — synthetische Mouse-Events lösen bei ApexCharts ohnehin keinen aus:

```js
// durch die Shadow-Roots nach <apexcharts-card> laufen, dann:
const chart = card[Object.keys(card).find(k => k.toLowerCase().includes('apexchart'))];
chart.w.config.tooltip.y.title.formatter("Platz 1");   // -> "firefox.exe"
typeof chart.w.config.tooltip.y.formatter === 'function'; // -> true, Einheit intakt
```

In den anderen Exportformaten stellt sich die Frage nicht: Prometheus bekommt
eine Serie je Zeile mit `app`- und `rank`-Label, InfluxDB ein Feld je Platz.

```
rig_top_cpu_percent{host="corganpc2",app="Cyberpunk2077.exe",rank="1"} 62.43
rig_top_cpu_percent{host="corganpc2",app="firefox.exe",rank="2"} 4.15
```

Wer die Werte langfristig als Diagramm braucht und HACS meiden will, ist mit
[Prometheus](export-targets.md#prometheus) und Grafana deutlich besser bedient als
mit Home Assistant.


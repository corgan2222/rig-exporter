# Exportziele

**Vier Wege, und Sie wählen frei: einen, mehrere oder alle gleichzeitig.** Sie
schließen sich nicht aus und wissen nichts voneinander — jedes Ziel lässt sich
einzeln an- und abschalten, ohne dass die anderen davon berührt werden.

| Ziel | Richtung | Voreingestellt |
|---|---|---|
| MQTT | Push — das Programm schickt | **an** |
| HTTP-Datenserver (JSON, Prometheus, Line Protocol) | Pull — der Empfänger holt | aus |
| Prometheus | Pull, über den Datenserver | an, sobald der Datenserver läuft |
| InfluxDB | Push — das Programm schreibt | aus |

Gemessen wird ohnehin nur einmal. Ein zweites Ziel einzuschalten kostet also
kaum etwas: dieselbe Momentaufnahme wird lediglich ein zweites Mal
hinausgeschrieben. Und sie sieht überall gleich aus — welche Quelle einen Wert
geliefert hat, erreicht keinen Export.

Ein üblicher Aufbau ist MQTT für Home Assistant *und* Prometheus für die
Langzeit-Diagramme in Grafana, weil Home Assistant für Wochen an
Zwei-Sekunden-Werten das falsche Werkzeug ist.

Ein- und ausgeschaltet wird das alles auf
[Export & Anzeige](interface/export-and-display.md); hier steht, was dabei
herauskommt und wie ein Empfänger es liest.

## MQTT

Standardmäßig an; die Zugangsdaten stehen unter
[Export & Anzeige → MQTT](interface/export-and-display.md#mqtt-push-an-home-assistant).
Es entsteht ein Gerät in Home Assistant, dessen Entities sagen, woher sie
kommen:

```
sensor.re_corganpc2_fps            sensor.re_corganpc2_gpu0_temperature
sensor.re_corganpc2_cpu            sensor.re_corganpc2_diskc_used_percent
sensor.re_corganpc2_game           sensor.re_corganpc2_net_ethernet_2_rx
sensor.re_corganpc2_ping_rtt       binary_sensor.re_corganpc2_rtss
```

Aufbau siehe [Wie die Bezeichner aufgebaut sind](identifiers.md#wie-die-bezeichner-aufgebaut-sind).

Die Kennung wird über `default_entity_id` angefordert. `object_id` tat das
früher und wurde in Home Assistant 2026 aus der MQTT-Komponente entfernt —
gesendet werden beide, damit ältere Fassungen weiter bedient sind. Ohne eines
von beiden baut Home Assistant die Kennung aus Gerätename und Entitätsname
zusammen, und aus `re_corganpc2_diskc_busy` wird `corganpc2_busy_c`.

Topics — `<node>` und die beiden Präfixe stellt man unter
[Export & Anzeige → Home Assistant](interface/export-and-display.md#home-assistant)
ein:

```
homeassistant/sensor/rig_<node>/<key>/config   Discovery, retained
rig-exporter/<node>/state                      ein JSON für alle Entities
rig-exporter/<node>/availability               online/offline, Last Will
```

Discovery folgt dem, was tatsächlich gemessen wurde: Wird Afterburner
nachträglich gestartet, sind die GPU-Entities beim nächsten Intervall da — ohne
Neustart. Verschwindet Hardware wieder, bleiben die Entities bestehen, damit
Historie und Dashboards nicht kaputtgehen. Stirbt der Prozess, setzt der Broker
über den Last Will alles auf `unavailable`.

**Der „Visit"-Link** auf der Geräteseite in Home Assistant zeigt auf die
Oberfläche dieses Exporters — und zwar auf den Port, auf dem sie *tatsächlich*
lauscht. Ist der eingestellte Port belegt, weicht der Webserver auf einen
zufälligen aus; die Discovery-Nachricht wird dann mit der richtigen Adresse neu
geschrieben, sobald sie feststeht. Das ist nötig, weil Discovery retained ist:
eine einmal mit dem falschen Port veröffentlichte Nachricht bliebe sonst falsch,
bis sie jemand überschreibt.

Welche Adresse dort steht, hängt davon ab, worauf der Server lauscht. In der
Voreinstellung ist das nur Loopback, also `http://127.0.0.1:<port>` — der Link
funktioniert dann, wenn Home Assistant im Browser **auf diesem PC** geöffnet
ist, vom Handy aus nicht. Ist
[**Diese Seite im Netzwerk erreichbar machen**](interface/export-and-display.md#anwendung)
gesetzt, steht dort die LAN-Adresse dieses Rechners und der Link funktioniert
von überall. Siehe [Oberfläche im Netzwerk](interface/on-the-network.md).

## HTTP-Datenserver

Ein zweiter Listener, standardmäßig `0.0.0.0:9838`, damit Home Assistant von
einem anderen Rechner zugreifen kann. Eingeschaltet wird er unter
[Export & Anzeige → Datenserver](interface/export-and-display.md#datenserver-home-assistant-und-prometheus-holen-ab).

Die drei Datenformate liefern dieselben Messwerte — die Wahl hängt nur daran,
wer sie abholt:

| Pfad | Format | Für wen | Eigenart |
|---|---|---|---|
| `/api/state` | **JSON**, ein Objekt mit allen Werten, identisch zum MQTT-State | RESTful-Sensor in Home Assistant, eigene Skripte | Am einfachsten zu lesen, aber in Home Assistant braucht **jeder einzelne Wert** einen eigenen Sensor-Eintrag mit Template |
| `/metrics` | **Prometheus** Text Exposition, eine Zeile je Zeitreihe | Prometheus, Grafana Agent, VictoriaMetrics | Bringt Typ und Hilfetext mit; mehrfach vorhandene Werte stehen als Labels (`gpu="0"`) statt als eigene Namen |
| `/influx` | **InfluxDB Line Protocol** | Telegraf und alles, was Line Protocol liest | Derselbe Inhalt wie der Push weiter unten, nur abgeholt statt geschickt |
| `/health` | `ok` als Text, sonst 503 | Uptime-Wächter, Container-Healthcheck | Antwortet **immer ohne Token** |
| `/` | Übersicht der aktiven Endpunkte | ein Mensch im Browser | Tokenpflichtig wie die Daten |

Optionaler Token, geprüft als `Authorization: Bearer <token>` oder `?token=`.
Ohne Token kann jeder im Netz die Werte lesen.

Ist ein Token gesetzt, schweigt der Port auch für die Übersichtsseite: sie nennt
Version und Node-ID, und das sind genau die Angaben, die einem Fremden sagen,
ob sich ein zweiter Blick lohnt. Nur `/health` antwortet weiter ohne Token —
ein Liveness-Check, der ein Geheimnis braucht, prüft nichts.

Läuft der Server, stehen die fertigen Adressen als anklickbare Links auf der
Anzeige und im Einstellungsblock. Sie tragen die IP-Adresse der Schnittstelle,
über die die Default-Route läuft, nicht den Rechnernamen — die IP funktioniert
auch dort, wo die Namensauflösung im lokalen Netz es nicht tut.

Home Assistant, `configuration.yaml`:

```yaml
rest:
  - resource: http://corganpc2:9838/api/state
    headers:
      Authorization: !secret rig_exporter_token   # nur wenn ein Token gesetzt ist
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

Teil des Datenservers: eingeschaltet wird er im selben Kasten, unter
*Prometheus-Exporter unter `/metrics`* — siehe
[Export & Anzeige → Datenserver](interface/export-and-display.md#datenserver-home-assistant-und-prometheus-holen-ab).

```yaml
scrape_configs:
  - job_name: rig-exporter
    scrape_interval: 5s
    static_configs:
      - targets: ["corganpc2:9838"]
    authorization:          # nur wenn ein Token gesetzt ist
      type: Bearer
      credentials: <token>
```

Jede Serie trägt `host="<node_id>"`, mehrere PCs kollidieren also nicht.
Instanzen werden zu Labels: `rig_disk_used_percent{host="corganpc2",disk="C:"}`,
`rig_gpu_temperature_celsius{host="corganpc2",gpu="0"}`,
`rig_net_receive_megabits_per_second{host="corganpc2",nic="Ethernet"}`.
Textwerte werden zu Info-Metriken: `rig_game_info{game="Cyberpunk2077.exe"} 1`.
Welcher Messwert welchen Namen bekommt, steht unter
[Bezeichner und Einordnung](identifiers.md).

## InfluxDB

Zwei Wege, unabhängig schaltbar.

**Pull** — `/influx` liefert Line Protocol, z. B. für Telegraf:

```toml
[[inputs.http]]
  urls = ["http://corganpc2:9838/influx"]
  data_format = "influx"
  headers = { Authorization = "Bearer <token>" }
```

**Push** — rig-exporter schreibt selbst an die InfluxDB-v2-Write-API
(`/api/v2/write`). Die Felder dafür — URL, Bucket, Organisation und Token, und
wie sie bei InfluxDB 1.8 anders belegt werden — stehen unter
[Export & Anzeige → InfluxDB](interface/export-and-display.md#influxdb-push).

Jede Gruppe wird ein eigenes Measurement, jede Instanz ein eigener Punkt:

```
rig,host=corganpc2 fps=143.2,cpu=24.5,ram=51.3,game="Cyberpunk2077.exe",resolution="2560x1440" …
rig_gpu,host=corganpc2,gpu=0 temperature=61,core_clock=2730,vram_used=5750,name="RTX 4090" …
rig_disk,host=corganpc2,disk=C: used_percent=77,read=0.4,write=3.6,media="NVMe" …
rig_net,host=corganpc2,nic=Ethernet rx=3.4,tx=0.13,ip="192.168.1.42" …
```

**Tag ist nur, was das gemessene Ding benennt:** der Rechner und die Instanz —
Laufwerk, Adapter, Grafikkarte, Speicherbank. „Schreiblast pro Platte" ist damit
ein `GROUP BY disk`.

**Alles andere ist ein Feld, auch wenn es Text ist** — Spielname, Auflösung,
IP-Adresse, Treiberversion. In InfluxDB *ist* die Tag-Menge die Kennung der
Reihe, und ein Text, der sich im Betrieb ändert, würde sie mitziehen: nach einem
IP-Wechsel liefen `rx_total` und `tx_total` desselben Adapters in einer zweiten
Reihe weiter, mit eigenen Summen von null an. Ein Spielname als Tag wäre
zusätzlich für immer im Index — jedes Spiel, das je lief.

Für eine Auswertung „pro Spiel" heißt das ein Feldvergleich statt eines
`GROUP BY`. Das ist der Preis, und er ist bewusst gezahlt: die Alternative sind
zerrissene Zählerreihen und ein Index, der mit jedem Spiel wächst.

---

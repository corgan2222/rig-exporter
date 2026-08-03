# rig-exporter

Telemetrie eines Gaming-PCs für Home Assistant, Prometheus und InfluxDB.

Liest die FPS aus dem RivaTuner Statistics Server, erkennt das laufende Spiel
und meldet dazu Grafikkarte, Prozessor, Laufwerke, Netzwerk und Latenz — per
MQTT-Autodiscovery, über einen eigenen HTTP-Datenserver, als
Prometheus-Exporter oder als InfluxDB Line Protocol. Alle vier gleichzeitig,
wenn man will.

Läuft als Tray-Anwendung ohne Konsolenfenster. Bedienung über eine
Weboberfläche, die nur auf `127.0.0.1` lauscht — deutsch oder englisch,
umschaltbar in der Kopfzeile.

> Hieß bis Version 1.0 `fps2mqtt`. Eine vorhandene Konfiguration wird beim
> ersten Start übernommen und die alten Home-Assistant-Entities werden
> automatisch entfernt.

---

## Was gemeldet wird

Es wird nur gemeldet, was der Rechner tatsächlich liefert. Fehlt die Quelle
einer Gruppe, entstehen dafür gar keine Entities — und sie erscheinen von
selbst, sobald die Quelle da ist. Jede Gruppe lässt sich einzeln abschalten.

| Gruppe | Werte | Quelle |
|---|---|---|
| **FPS & System** (immer an) | FPS, Frametime, laufendes Spiel, Auflösung, Bildwiederholrate, CPU-Last, RAM-Last, Laufzeit, Leerlaufzeit | RTSS + Windows |
| **Grafikkarte** | Temperatur, Hotspot, Kern- und Speichertakt, Auslastung, VRAM, Lüfter (% und U/min), Leistung, Leistungsgrenze und deren Ausschöpfung, Spannung — pro Karte | MSI Afterburner, ersatzweise NVML |
| **Prozessor** | Modell, Kerne, Threads, Basis-, wirksamer und höchster beobachteter Takt, Temperatur, Load über 1/5/15 Minuten, optional Last je Thread | Windows, Temperatur über Afterburner |
| **Arbeitsspeicher** | belegt und frei in MB, frei in %, gesamt, Takt, maximaler Takt, Typ, bestückte und vorhandene Steckplätze, ein Eintrag je Modul | Windows + SMBIOS der Firmware |
| **Laufwerke** | Typ (NVMe/SSD/HDD), Label, Kapazität, belegt, frei, Belegung und freier Anteil in %, Lesen, Schreiben, Auslastung — pro Volume | Windows |
| **Netzwerk** | Adapter, Link-Speed, Durchsatz, Fehler, verworfene Pakete, WLAN-Signal, Ping und Paketverlust | Windows + ICMP |

Auf dem Entwicklungsrechner ergibt das 91 Werte: zwei Grafikkarten, vier NVMe,
ein aktiver Adapter.

CPU- und RAM-Last gehören zu **FPS & System**, damit sie unabhängig von jedem
Schalter da sind — die Kacheln oben auf der Seite brauchen sie. Angezeigt werden
sie trotzdem bei Prozessor und Arbeitsspeicher, weil dort danach gesucht wird.
Ein zweiter Sensor mit demselben Wert wäre die Alternative, und zwei
Home-Assistant-Entitäten für dieselbe Zahl sind schlechter als keine.

### Woher die Grafikwerte kommen

Windows selbst gibt weder GPU-Temperatur noch Takt her — dafür braucht es einen
Treiber. Deshalb:

1. **MSI Afterburner** (`MAHMSharedMemory`) ist die erste Wahl: deckt NVIDIA,
   AMD und Intel ab, liefert Lüfter, Spannung und Hotspot. RTSS gehört ohnehin
   dazu, ein für den FPS-Overlay eingerichteter Rechner hat das also schon.
2. **NVML** aus dem NVIDIA-Treiber füllt die Lücken, vor allem den
   VRAM-Gesamtausbau und die Lüfterdrehzahl. Ohne Afterburner reicht es allein
   für NVIDIA-Karten.

Ohne Afterburner fehlen von der GPU nur Hotspot-Temperatur und Spannung — den
Rest liefert NVML, Lüfterdrehzahl eingeschlossen (`nvmlDeviceGetFanSpeedRPM`,
gemeldet wird der schnellste Lüfter der Karte). NVML wächst mit jeder
Treibergeneration um neue Einsprungpunkte, und `LazyProc.Call` löst das Symbol
über `mustFind` auf — das **panict**, wenn es fehlt. In einem Binary mit
`-H windowsgui` stirbt damit das Tray wortlos. Deshalb wird jeder Einsprungpunkt
einmal aufgelöst und vor dem ersten Aufruf geprüft; ein alter Treiber verliert
einen Wert, nicht das Programm.

Ist beides nicht da, entfällt die GPU-Gruppe. Ohne Kernel-Treiber sind
Gehäuselüfter, Netzteil-Telemetrie und Spannungen grundsätzlich nicht
erreichbar.

Beide zählen unabhängig voneinander durch, verbunden werden sie deshalb über
den Kartennamen — und der ist nicht eindeutig, zwei gleiche Karten heißen
gleich. Zugeordnet wird darum in Indexreihenfolge, jede Instanz höchstens
einmal, und eine Karte, für die sich kein Name findet, bekommt eine eigene
Instanz, statt eine fremde zu überschreiben. Auf einem Notebook, dessen
integrierte Grafik bei Afterburner Karte 0 ist, wäre sonst deren Eintrag mit dem
VRAM und der Leistungsgrenze der dedizierten Karte überschrieben worden.

### Der aktive Netzwerkadapter

Standardmäßig wird nur der Adapter gemeldet, über den die Default-Route läuft.
Ein Rechner mit Hyper-V, WSL, VPN und Capture-Treiber hat sonst schnell ein
Dutzend Interfaces, und das eine, das zählt, geht darin unter. Umschaltbar über
**Alle Adapter statt nur dem aktiven**.

Ping und Paketverlust laufen in einem eigenen Takt, unabhängig vom
Sendeintervall: eine Runde gegen einen nicht erreichbaren Host dauert Sekunden
und darf die Messschleife nicht blockieren. Ziel ist standardmäßig das
Default-Gateway.

---

## Voraussetzungen

* Windows 10/11 (64 Bit)
* [RivaTuner Statistics Server](https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/) für die FPS — auch in MSI Afterburner enthalten
* [MSI Afterburner](https://www.msi.com/Landing/afterburner/graphics-cards) für GPU- und CPU-Temperaturen
* Zum Bauen: Go 1.22 oder neuer. Kein CGO, kein C-Compiler.

Fehlt RTSS, erscheint **beim ersten Start** ein Hinweis mit Downloadlink —
danach nicht mehr. Alle übrigen Gruppen laufen ohne RTSS weiter, und der Zustand
steht im Tray und auf der Anzeigeseite. Ein Rechner ohne RTSS ist für alles
andere ein völlig brauchbarer Rechner, und nichts wartet hier auf einen Dialog.

Nichts am Programm braucht Administratorrechte. Läuft RTSS oder Afterburner
allerdings eleviert und dieses Programm nicht, verweigert Windows den Zugriff
auf deren Shared Memory — dann fehlen FPS beziehungsweise die Temperaturen.

Wird RTSS geschlossen, verschwindet sein Shared Memory **nicht**: RTSSHooks
bleibt in jeder eingeklinkten Anwendung geladen, der Abschnitt überlebt den
Prozess, und RTSS überschreibt beim Beenden seine Signatur mit `0xDEAD` —
laut eigenem SDK „zur Freigabe markiert". Das wird als „läuft nicht" gemeldet,
nicht als Fehler. Startet RTSS später, verbindet sich das Programm von selbst:
die Zuordnung wird bei jedem Auslesen neu geöffnet, ein Neustart ist nie nötig.

### PawnIO

PawnIO ist ein signierter Kerneltreiber, der geprüften Bytecode ausführt — der
sichere Nachfolger von WinRing0, das wegen freien Registerzugriffs auf
Microsofts Treiber-Sperrliste steht. Damit wären Prozessortemperatur und
-leistung auch ohne Afterburner lesbar.

Erkannt wird es ohne jedes Recht: `PawnIOLib.dll` lädt und meldet ihre Version
auch aus einem gewöhnlichen Prozess. **Benutzen** lässt sie sich so aber nicht.
PawnIOs Gerät trägt eine geschützte ACL, `D:P(A;;GA;;;SY)(A;;GA;;;BA)` — nur
LocalSystem und Administratoren. Nachgemessen: `pawnio_open` liefert aus einem
nicht-elevierten Prozess `0x80070005`, E_ACCESSDENIED.

Daraus folgt die Aufteilung. Erkennung läuft immer und unterscheidet vier
Zustände, weil sie zu vier verschiedenen Ratschlägen führen: nicht installiert,
installiert aber ohne Adminrechte erreichbar, Treiber antwortet nicht, nutzbar.
Jemandem „installier es" zu sagen, der es längst hat, ist schlechter als
schweigen.

Eingeschaltet wird es nur bewusst, in den Einstellungen. Aus, solange niemand
zustimmt: es einzuschalten heißt, rig-exporter mit Administratorrechten laufen
zu lassen, und das ist eine Entscheidung über den Rechner, keine Einstellung.

Beim ersten Start und nur dann, wenn PawnIO fehlt, erscheint ein Angebot. Es
sagt ausdrücklich, dass ein Kerneltreiber installiert wird und dass danach
Adminrechte nötig sind, und es nennt MSI Afterburner als treiberfreie
Alternative. Wer zustimmt, bekommt das Installationsprogramm heruntergeladen —
geprüft wird dabei, dass die Weiterleitungskette wirklich auf einem
GitHub-Release-Host per HTTPS endet. Ausgeführt wird es **nicht** von diesem
Programm: es geht per `ShellExecute` an Windows, damit Signaturprüfung,
SmartScreen und die Rechteabfrage dort stattfinden, wo man sie sieht.

PawnIO wird nicht mitgeliefert. Es steht unter GPL-2.0, die Module unter
LGPL-2.1; installiert wird es vom Nutzer, dieses Programm sucht es nur.

**CPU-Temperatur gibt es sonst nur mit Afterburner.** Das ist keine Bequemlichkeit:
Ryzen liefert Tctl über den SMU, Intel über ein MSR, und beides liegt in Ring 0.
Kein Programm ohne Kerneltreiber kommt daran — deshalb bringt Afterburner einen
mit. Die treiberfreien Wege sind nachgemessen und alle tot: ACPI-Thermalzonen
(über PDH, SetupDi und WMI je null Instanzen), `Win32_TemperatureProbe` (braucht
eine SMBIOS-Struktur, die Consumer-Boards nicht schreiben) und
`CallNtPowerInformation` (hat kein Temperaturfeld).

### Was auf anderen Maschinen anders ist

Getestet wird auf einem Rechner: Windows 10, deutsch, Ryzen, zwei
NVIDIA-Karten, amd64. Was bekannt ist:

* **Sprache** — die Oberfläche folgt beim ersten Start der Windows-Sprache und
  ist danach umschaltbar. Gemeldete *Werte* bleiben englisch (`Ethernet`,
  `Wi-Fi`, `Other`, `DDR4`, `Type 126`), damit eine Automatisierung in Home
  Assistant nicht davon abhängt, welche Sprache gerade eingestellt ist.
* **Hybrid-CPUs** (Intel 12th gen und neuer, P- und E-Kerne) — der gemeldete
  Takt ist dort **systematisch zu hoch**. Der Leistungsindikator mittelt
  Verhältnisse gegen je eigene Nominalfrequenzen, hier wird aber mit einem
  einzigen Basistakt multipliziert. Ungetestet, weil keine solche Maschine da
  ist; alle übrigen CPU-Werte stimmen.
* **Mehr als 64 logische Prozessoren** — Kernzahl und Auslastung stimmen; nur
  die optionale Liste je Kern deckt eine Prozessorgruppe ab.
* **Andere Architekturen** — `arm64` und `386` übersetzen, sind aber nie
  gelaufen, und das Symbol der Exe gibt es nur für `amd64`.

## Bauen

```powershell
.\build.ps1 -Check
```

`-Check` erzeugt das Icon neu, prüft Formatierung, führt `go vet` und die Tests
aus und baut danach. Ohne Flag wird nur gebaut. Ergebnis ist ein einzelnes
`rig-exporter.exe` (~11 MB) ohne weitere Dateien.

`tools/genicon` erzeugt zwei Dinge aus derselben Zeichnung: `icon.ico` für den
Infobereich und `rsrc_windows_amd64.syso`, die Windows-Ressourcendatei, die der
ausführbaren Datei ihr Symbol in Explorer, Taskleiste und Alt-Tab gibt. Beide
sind eingecheckt, damit ein blankes `go build` reicht — geschrieben werden sie
von Hand statt mit `rsrc` oder `goversioninfo`, damit man kein Werkzeug
installieren muss.

## Erster Start

1. `rig-exporter.exe` starten — ein Tacho-Symbol erscheint im Infobereich.
2. Rechtsklick → **Einstellungen…** öffnet <http://127.0.0.1:8787> im Browser.
3. Exportziel und Sensorgruppen wählen, **Speichern & übernehmen**.

Konfiguration und Log liegen in `%APPDATA%\rig-exporter`.

## Oberfläche

Drei Seiten, erreichbar über die Kopfzeile:

* **Anzeige** — Live-Werte, Zustand der Exportziele, ein Panel je Sensorgruppe
  und die Adressen der aktiven Endpunkte. Aktualisiert sich im
  Auslese-Intervall.
* **Datengewinnung** — welche Sensorgruppen gelesen werden und wie oft.
* **Export & Anzeige** — wohin die Werte gehen (MQTT, Home Assistant,
  Datenserver, InfluxDB) und wie sich die Anwendung selbst verhält.

Jeder Abschnitt hat einen eigenen Speichern-Button, der erst grün wird, wenn in
genau diesem Abschnitt etwas geändert wurde. Gespeichert wird auch nur dieser
eine Abschnitt: ein Formular trägt keinen Beleg über Kästchen, die es gar nicht
enthält, und eine Teilübernahme würde sonst alles auf der anderen Seite
abschalten.

In den Hardware-Panels lässt sich zwischen zwei Sortierungen umschalten: **nach
Messwert** listet gleichartige Werte untereinander, **nach Gerät** stellt alles
zu GPU 0 zusammen, dann alles zu GPU 1, jede Platte für sich, jeder Adapter für
sich. Die Wahl bleibt im Browser gespeichert.

Rechts in der Kopfzeile steht der Sprachumschalter. Er wirkt sofort und auf
alles: Oberfläche, Tray-Menü, Dialoge und die Entity-Namen in Home Assistant.
Entity-IDs bleiben davon unberührt — `sensor.fps_corganpc2` heißt in beiden
Sprachen gleich, nur der angezeigte Name wechselt. Dashboards und Automationen
überleben einen Sprachwechsel also unbeschadet.

Nicht übersetzt wird, was Maschinen lesen: Prometheus-Hilfetexte, Logzeilen und
Fehlermeldungen bleiben englisch.

Unten auf jeder Seite öffnen drei Schaltflächen die Konfiguration, das Log und
den Ordner darum. Der Umweg über den Server ist nötig, weil ein Browser einem
`file://`-Link von einer `http`-Seite aus nicht folgt.

## Auslesen und Senden

Zwei getrennte Takte:

| Einstellung | Bedeutung | Standard |
|---|---|---|
| **Auslese-Intervall** | wie oft die Hardware abgefragt wird | 500 ms |
| **Sendeintervall** | wie oft ein Messwert die Maschine verlässt | 2000 ms |

Das Auslesen bestimmt, wie flüssig Tray und Anzeige laufen; das Senden
bestimmt, wie viel bei Broker und Zeitreihendatenbank ankommt. Wer im Tray eine
lebendige FPS-Zahl will, ohne Home Assistant mit vier Werten pro Sekunde zu
fluten, stellt 250 ms und 2000 ms ein.

Das Sendeintervall wird auf ein ganzzahliges Vielfaches des Auslese-Intervalls
aufgerundet und ist nie kürzer als dieses. Gezählt wird in Messungen, nicht in
einer zweiten Uhr — die beiden können also nicht auseinanderdriften.

---

## Exportziele

### 1. MQTT (Push)

Standardmäßig an. Ein Gerät in Home Assistant, dessen Entities nach dem PC
benannt sind:

```
sensor.fps_corganpc2              sensor.gpu_temperature_0_corganpc2
sensor.cpu_corganpc2              sensor.disk_used_percent_c_corganpc2
sensor.game_corganpc2             sensor.net_rx_ethernet_corganpc2
sensor.ping_rtt_corganpc2         binary_sensor.rtss_corganpc2
```

Der Teil vor dem Hostnamen ist der Messwert, ein angehängter Buchstabe oder
Index die Instanz — `disk_used_percent_c` ist die Belegung von `C:`.

Topics:

```
homeassistant/sensor/rig_<node>/<key>/config   Discovery, retained
rig-exporter/<node>/state                      ein JSON für alle Entities
rig-exporter/<node>/availability               online/offline, Last Will
```

Discovery folgt dem, was tatsächlich gemessen wurde: Wird Afterburner
nachträglich gestartet, sind die GPU-Entities beim nächsten Intervall da — ohne
Neustart. Verschwindet Hardware wieder, bleiben die Entities bestehen, damit
Historie und Dashboards nicht kaputtgehen. Stirbt der Prozess, setzt der Broker
über den Last Will alles auf `unavailable`. Beim Ändern von Node-ID oder Präfix
werden die alten Entities selbst entfernt.

### 2. HTTP-Datenserver (Pull)

Ein zweiter Listener, standardmäßig `0.0.0.0:9838`, damit Home Assistant von
einem anderen Rechner zugreifen kann. Standardmäßig **aus**.

| Pfad | Inhalt |
|---|---|
| `/api/state` | JSON, identisch zum MQTT-State |
| `/metrics` | Prometheus Text Exposition |
| `/influx` | InfluxDB Line Protocol |
| `/health` | Liveness-Check, nie tokenpflichtig |
| `/` | Übersicht der aktiven Endpunkte |

Optionaler Token, geprüft als `Authorization: Bearer <token>` oder `?token=`.
Ohne Token kann jeder im Netz die Werte lesen.

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
        value_template: "{{ value_json.gpu_temperature_0 }}"
        unit_of_measurement: "°C"
        device_class: temperature
        state_class: measurement
      - name: SSD C CorganPC2
        unique_id: disk_c_corganpc2_rest
        value_template: "{{ value_json.disk_used_percent_c }}"
        unit_of_measurement: "%"
        state_class: measurement
```

### 3. Prometheus

Teil des Datenservers:

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

### 4. InfluxDB

Zwei Wege, unabhängig schaltbar.

**Pull** — `/influx` liefert Line Protocol, z. B. für Telegraf:

```toml
[[inputs.http]]
  urls = ["http://corganpc2:9838/influx"]
  data_format = "influx"
  headers = { Authorization = "Bearer <token>" }
```

**Push** — rig-exporter schreibt selbst an die InfluxDB-v2-Write-API
(`/api/v2/write`). URL, Bucket, Organisation und Token in den Einstellungen.
InfluxDB 1.8 versteht dieselbe API: Organisation leer lassen, als Token
`benutzer:passwort` eintragen.

Jede Gruppe wird ein eigenes Measurement, jede Instanz ein eigener Punkt:

```
rig,host=corganpc2,game=Cyberpunk2077.exe,resolution=2560x1440 fps=143.2,cpu=24.5,ram=51.3 …
rig_gpu,host=corganpc2,gpu=0,name=RTX\ 4090 temperature=61,core_clock=2730,vram_used=5750 …
rig_disk,host=corganpc2,disk=C:,media=NVMe used_percent=77,read=0.4,write=3.6 …
rig_net,host=corganpc2,nic=Ethernet rx=3.4,tx=0.13 …
```

Spiel, Laufwerk und Adapter sind Tags — „durchschnittliche FPS pro Spiel" oder
„Schreiblast pro Platte" ist damit ein `GROUP BY` statt ein String-Vergleich.

---

## Tray-Menü

Zeigt FPS, Spiel, Anzeige und Auslastung inklusive GPU live, dazu eine
Statuszeile je aktivem Exportziel und den RTSS-Status. Der Name ganz oben
öffnet die Oberfläche im Browser. Weitere Aktionen: Senden pausieren,
Einstellungen öffnen, Log öffnen, Autostart mit Windows, Beenden. Fehlt RTSS,
kommt ein Eintrag zum Download dazu.

## Diagnose

```bash
.\rig-exporter.exe -probe
```

Nimmt zwei Messungen im Abstand von vier Sekunden und schreibt alles heraus,
gruppiert nach Sensorgruppe, gefolgt von JSON, Prometheus-Exposition und Line
Protocol. Der schnellste Weg zu sehen, welche Quellen greifen und was bei Home
Assistant ankäme.

Die Ausgabe landet **immer zusätzlich** in `%APPDATA%\rig-exporter\probe.txt`,
und das hat einen Grund. Das Programm ist als GUI-Anwendung gelinkt, damit beim
Start kein Konsolenfenster aufblitzt; es hat deshalb von sich aus keine Konsole
und leiht sich die des aufrufenden Terminals. Wie die Ausgabe aufgefangen wird,
hängt danach von der Shell ab: PowerShells `>` erzeugt bei einem GUI-Programm
stillschweigend eine leere Datei, während `| Out-File`, `cmd /c >` und der
direkte Aufruf funktionieren. Eine Diagnose, deren Ergebnis davon abhängt,
welche Umleitung jemand getippt hat, ist keine — darum gibt es immer eine Datei,
und ihr Pfad steht am Ende der Ausgabe.

| Symptom | Ursache |
|---|---|
| RTSS `not_running` | RTSS ist nicht gestartet. |
| RTSS `access_denied` | RTSS läuft erhöht, rig-exporter nicht. Eines von beiden angleichen. |
| FPS bleibt 0, Spiel `none` | RTSS hookt die Anwendung nicht. Im RTSS-Profil „Application detection level" prüfen. |
| Keine GPU-Gruppe | Weder Afterburner noch NVML erreichbar. Läuft Afterburner erhöht, gilt dasselbe wie bei RTSS. |
| Keine CPU-Temperatur | Kommt nur über Afterburner. |
| Keine Durchsatzwerte | Erst ab der zweiten Messung vorhanden, sie sind eine Differenz. |
| Entities fehlen in HA | MQTT-Integration aktiv? Discovery-Präfix identisch? Log prüfen. |

## Wie die Werte zustande kommen

**FPS und Spiel** kommen aus `RTSSSharedMemoryV2`. Der Block wird bei jedem
Intervall neu gemappt, gelesen und freigegeben, ein RTSS-Neustart also ohne
Zutun aufgefangen. Die Rate ist `1000 × Frames / (Time1 − Time0)`, genau das,
was der Overlay anzeigt.

Von allen gehookten Prozessen gewinnt der Vordergrundprozess, wenn RTSS ihn
kennt — das ist, worauf man gerade schaut. Sonst der zuletzt gerenderte, damit
ein Spiel im Hintergrund weiterzählt. Einträge, deren letztes Bild älter als das
Idle-Timeout ist, fallen raus; das lässt ein beendetes Spiel auf `none`
zurückfallen statt beim letzten Wert einzufrieren.

**GPU** kommt aus Afterburners Shared Memory. Die Sensornamen sind pro Karte
durchnummeriert (`GPU1 temperature`), und der Kartenindex im Eintrag ist nicht
verlässlich — Afterburner setzt ihn auch bei „RAM usage" —, deshalb wird
ausschließlich über den Namen zugeordnet. NVML-Karten werden über den Namen mit
den Afterburner-Karten gepaart, nicht über den Index: zwei Karten
verschiedener Hersteller würden sonst vertauscht.

**Auflösung und Hz** kommen von `EnumDisplaySettingsW` des primären Monitors,
also aus dem Anzeigetreiber — unabhängig von der DPI-Skalierung des Prozesses.

**CPU-Last** ist die Differenz zweier `GetSystemTimes`-Abfragen, die Last je
Thread kommt aus `NtQuerySystemInformation`.

**CPU-Takt** ist der wirksame Takt, nicht der Basistakt. `CallNtPowerInformation`
wäre der naheliegende Weg, liefert auf jedem aktuellen AMD und den meisten Intel
aber unverändert den Nennwert — ein 5950X mit 4,2 GHz meldet dort seine 3,4 GHz
Basistakt, und die Anzeige steht still. Der einzige bewegliche Wert ist der
Leistungsindikator `% Processor Performance`, ein Prozentsatz des Basistakts,
der beim Boosten über hundert geht. Gelesen wird er über PDH mit
`PdhAddEnglishCounterW`, weil Indikatornamen übersetzt sind und derselbe Zähler
auf einem deutschen Windows `% Prozessorleistung` heißt. Aus ihm ergeben sich
drei Werte: **Basistakt** (der Nennwert aus der Registry), **Takt** (der
wirksame) und **Takt max.**, der höchste seit dem Start beobachtete — den
Boost-Takt nennt Windows nirgends, beobachten lässt er sich aber. Zwei Abfragen
kurz hintereinander teilen zwei Differenzen nahe null durch einander, was
Ausreißer ergibt, die sich im Maximum dauerhaft festsetzen würden; darum wird
ein Messfenster von mindestens 100 ms verlangt. Fällt der Indikator aus, bleibt
der Nennwert aus `CallNtPowerInformation` als Rückfallebene.

**Load** gibt es unter Windows nicht: es existiert keine Lauf-Warteschlange,
die man auslesen könnte. Gemessen wird deshalb dasselbe von der anderen Seite —
Auslastung mal Anzahl logischer Prozessoren, also wie viele Prozessoren an
Arbeit tatsächlich verrichtet werden. Load 4 auf einer 16-Thread-Maschine
bedeutet vier Threads voll ausgelastet, genau wie unter Linux. Was diese Zahl
nicht zeigen kann, ist eine Warteschlange, die länger ist als die Maschine
breit — bei Volllast deckelt sie bei der Kernzahl. Geglättet wird mit denselben
Konstanten wie unter Linux, und zwar über die tatsächlich verstrichene Zeit:
ein anderes Auslese-Intervall ändert nicht, was ein Ein-Minuten-Mittel bedeutet.

**GPU-Leistungsgrenze** ist das erzwungene Board-Power-Limit aus NVML — die
Zahl, die man meint, wenn man TDP sagt. Zusammen mit der aktuellen Aufnahme
ergibt sie den Prozentsatz, an dem man sieht, ob die Grenze gerade bremst.

**Arbeitsspeicher**: Belegung und freier Speicher aus `GlobalMemoryStatusEx`.
Takt, Typ und Bestückung kennt Windows nicht — die stehen in den
SMBIOS-Tabellen, die über `GetSystemFirmwareTable` erreichbar sind und hier
selbst geparst werden, statt den Umweg über WMI und COM zu nehmen. Der Takt ist
der konfigurierte, also der, auf den sich der Controller eingependelt hat; bei
gemischter Bestückung ist das der des langsamsten Riegels. Die Slot-Bezeichnung
wiederholt sich auf den meisten Boards je Kanal, deshalb identifiziert erst
Kanal plus Bezeichnung einen Steckplatz.

**Laufwerke**: Belegung aus `GetDiskFreeSpaceEx`, Typ über
`IOCTL_STORAGE_QUERY_PROPERTY` (Bustyp und Seek-Penalty), Durchsatz aus
`IOCTL_DISK_PERFORMANCE`. Das Volume wird dafür ganz ohne Zugriffsrechte
geöffnet — genau das erlaubt die Abfrage ohne Adminrechte.

**Netzwerk**: Adapter aus `GetAdaptersAddresses`, Zähler aus `GetIfTable2`,
WLAN-Signal aus `wlanapi`, Latenz aus `IcmpSendEcho`. Alle Zählerdifferenzen
fangen einen Reset ab: geht ein Zähler zurück, ist das Intervall 0 und nicht
vier Milliarden Ereignisse pro Sekunde.

Fehler- und Verwurfszähler sind die, die Treiber am häufigsten falsch führen —
der Realtek-Adapter im Testrechner meldet 267 Billionen empfangene Verwürfe mit
zwei Milliarden Zuwachs pro Sekunde. Werte oberhalb dessen, was die
Verbindungsgeschwindigkeit physikalisch hergibt, werden deshalb weggelassen
statt gemeldet: eine fehlende Entity ist ehrlicher als eine mit Unsinn darin.
Der Ping steht bei einem Gateway im LAN oft auf `0 ms`, weil Windows die
Laufzeit nur in ganzen Millisekunden zurückgibt.

## Aufbau

```
main.go                          Start, Einzelinstanz-Sperre, RTSS-Check, -probe
internal/i18n                    Sprachen: Katalog der Oberflächentexte
internal/metrics                 die Messwertdefinition, aus der alle Formate entstehen
internal/collector               eine Messung aus Kern- und optionalen Quellen
internal/rtss                    RTSS Shared Memory
internal/sysinfo                 CPU-Last, RAM, Anzeigemodus, Leerlauf, Laufzeit
internal/hardware/afterburner    Afterburner Shared Memory
internal/hardware/gpu            GPU-Gruppe: Afterburner + NVML
internal/hardware/cpu            CPU-Gruppe
internal/hardware/ram            Speichergruppe, inklusive SMBIOS-Parser
internal/hardware/disk           Laufwerksgruppe
internal/hardware/net            Netzwerkgruppe und Latenzmessung
internal/export                  gemeinsame Schnittstelle der Exportziele
internal/export/dataserver       HTTP: JSON, Prometheus, Influx
internal/export/influxpush       Schreiben an InfluxDB
internal/hamqtt                  MQTT und Home-Assistant-Discovery
internal/app                     Messschleife, Konfigurationswechsel
internal/webui                   Einstellungsseite auf 127.0.0.1
internal/tray                    Infobereich-Symbol und Menü
internal/winapi                  die Win32-Aufrufe, die x/sys nicht abdeckt
tools/genicon                    erzeugt internal/assets/icon.ico
```

Ein neuer Messwert wird einmal in `internal/metrics` eingetragen und erscheint
danach in allen vier Formaten — inklusive Home-Assistant-Darstellung, denn
Einheit, Device-Class und Icon stehen in derselben Definition. Der Name steht
dort gleich zweisprachig, statt in einer entfernten Tabelle; Oberflächentexte
ohne natürlichen Ort liegen im Katalog in `internal/i18n`. Ein Test bricht,
sobald eine Übersetzung fehlt.

Eine dritte Sprache ist entsprechend: ein Feld an `i18n.Text`, ein Eintrag in
`i18n.Available` — der Test zeigt dann jede Stelle, an der noch etwas fehlt.

## Tests

```bash
go test ./...
```

Laufen ohne RTSS, ohne Afterburner, ohne Broker und ohne InfluxDB: die drei
Parser (RTSS, Afterburner, SMBIOS) werden gegen synthetische Speicherblöcke
geprüft, die Exporter und Web-Handler gegen `httptest`-Server, die Messquellen
gegen Attrappen.

Abgedeckt sind: die Parser, die Metrikdefinition und ihre vier Ausgabeformate,
die Konfiguration samt Migration und Grenzwerten, die Übersetzungen, die
Home-Assistant-Discovery, die Exportziele, die Messschleife mit ihren zwei
Takten, die Web-Handler samt blockweisem Speichern, und der Load-Mittelwert.

Nicht abgedeckt sind die Win32-Aufrufe selbst und die Windows-Hälften der
Hardware-Quellen — die brauchen die Hardware, die sie beschreiben. Sie werden
mit `-probe` gegen den echten Rechner geprüft. Auch das Tray-Menü und das
Icon-Werkzeug sind nur manuell verifiziert.

`go vet` läuft im Build-Skript mit `-unsafeptr=false`: das Mappen fremder
Shared-Memory-Blöcke braucht eine `uintptr`-Konvertierung, die vet nicht
beurteilen kann. Strukturen, deren Größe exakt zur Windows-Definition passen
muss, sichern sich stattdessen selbst über eine Größenzusicherung ab, die zur
Übersetzungszeit bricht.

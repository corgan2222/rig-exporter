# Eigene Messwerte hinzufügen

!!! tip "Sie wollen nur an die vorhandenen Daten?"

    Dann brauchen Sie diese Seite nicht. Wie Sie die Werte per JSON, Prometheus,
    Line Protocol oder MQTT abholen, steht unter
    [Exportziele](export-targets.md) — dafür ist nichts zu programmieren.

Diese Seite beschreibt den anderen Fall: Sie wollen einen Wert **melden**, den
das Programm noch nicht kennt. Ein Sensor am Mainboard, ein Wert aus einer
eigenen Anwendung, eine Zahl aus einer Datei.

## Warum das wenig Arbeit ist

Es gibt **keinen** exportspezifischen Code zu schreiben. Ein Messwert wird an
genau einer Stelle beschrieben, und daraus folgt alles Weitere von selbst:
MQTT-Discovery in Home Assistant, das JSON unter `/api/state`, die
Prometheus-Exposition unter `/metrics` und das Line Protocol für InfluxDB. Die
Exporter kennen keine einzelne Messgröße, sie kennen nur den Katalog.

Das ist auch die Zusage, die Sie dabei nicht brechen dürfen: **das Ausgabeformat
ist über alle Datenquellen identisch.** Woher ein Wert kam, erreicht keinen
Export.

## Die vier Schritte

### 1. Den Messwert beschreiben

In `internal/metrics/definitions.go` eine `Definition` anlegen und sie unten in
`All` eintragen — die Reihenfolge dort ist die Katalogreihenfolge und damit auch
die Anzeigereihenfolge.

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

Die Felder, bei denen man sich vertun kann:

| Feld | Regel |
|---|---|
| `ID` | Kurz, klein geschrieben, **niemals übersetzt**. Daran hängen Entity-IDs, Dashboards und Automatisierungen |
| `Name` | Genau umgekehrt: folgt immer der Sprache |
| `Kind` | `KindGauge`, `KindText`, `KindBool` oder `KindTable` — letzteres eine kurze Rangliste mit einem Namen und einer Zahl je Zeile. Bestimmt, ob daraus ein `sensor` oder ein `binary_sensor` wird |
| `Group` | Entscheidet, **welcher Schalter den Wert abschaltet** — `GroupGPU`, `GroupCPU`, `GroupRAM`, `GroupDisk`, `GroupNet`, `GroupCooling`, `GroupBattery`. `GroupCore` wird immer erhoben und hat keinen Schalter; solche Werte lassen sich nur über die Messwertauswahl abwählen |
| `Panel` | Nur nötig, wenn der Wert woanders angezeigt als erhoben wird |
| `InstanceLabel` | Setzen, sobald es den Wert mehrfach gibt (`"gpu"`, `"disk"`, `"nic"`, `"core"`) |
| `EntityCategory` | `diagnostic` für Tatsachen *über* die Maschine, leer für Messungen *an* ihr |
| `NoEntity` | Hält den Wert aus Home Assistant heraus, ohne ihn aus JSON, Prometheus und InfluxDB zu nehmen |

`Prom` und `Help` bleiben **englisch**. Das ist ein maschinenlesbares Format,
gelesen von einem Scraper und von dem, der die Abfrage schreibt.

### 2. In die Umfänge einsortieren

`internal/metrics/sets.go`. **Erweitert** ist der ganze Katalog und braucht
nichts — der neue Wert ist automatisch dabei. Wer ihn auch in **Minimal** oder
**Standard** haben will, trägt seine ID in `minimalSet` beziehungsweise
`basicSet` ein.

Die Rungen sind bewusst gepflegte Listen und keine Regel über die Definitionen,
weil keine Regel es richtig trifft: `diagnostic` beschreibt, wo Home Assistant
eine Entität ablegt, nicht ob jemand sie sehen will. Sie schachteln ineinander —
Minimal ⊂ Standard ⊂ Erweitert — und ein Test hält das fest.

### 3. Die Quelle schreiben

Eine Quelle ist alles, was zwei Methoden hat:

```go
type Source interface {
    Group() metrics.Group
    Collect(set *metrics.Set) error
}
```

`Collect` **hängt an** das Set, statt eine Liste zurückzugeben. Das ist Absicht:
eine Quelle, die halb durchkommt — drei von vier Platten lesbar —, meldet
trotzdem, was sie hat. Ein Fehler ist eine Diagnose, kein Grund, die Messwerte
wegzuwerfen.

```go
func (s *Mainboard) Group() metrics.Group { return metrics.GroupCPU }

func (s *Mainboard) Collect(set *metrics.Set) error {
    celsius, err := s.read()
    if err != nil {
        return err          // kein set.Add — der Wert fehlt dann einfach
    }
    set.Add(metrics.Gauge(metrics.MainboardTemperature, "", celsius))
    return nil
}
```

Der zweite Parameter von `Gauge` ist die Instanz und bleibt leer, solange es den
Wert nur einmal gibt. Für Text, Wahrheitswerte und Ranglisten gibt es
`metrics.Text`, `metrics.Bool` und `metrics.Table`.

Drei Regeln, an denen sich sonst jemand die Finger verbrennt:

!!! warning "Fehlende Werte weglassen, nicht nullen"

    Eine 0 behauptet, es seien null Grad. Ein fehlendes Feld behauptet nichts.
    Im Zweifel gar nichts hinzufügen.

!!! warning "Die Uhr läuft"

    Jede Quelle bekommt **die Hälfte eines Auslese-Intervalls** — bei den
    voreingestellten 500 ms also 250 ms. Wer länger braucht, wird abgeschnitten
    und meldet „keine Antwort". Eine langsame Quelle gehört deshalb auf eine
    eigene Goroutine, die in einen Zwischenspeicher schreibt; `Collect` liest
    dann nur noch diesen Speicher. Die Kühlungsquelle macht genau das.

!!! warning "Eine Panik legt Ihre Quelle still"

    Panikt eine Quelle, fängt der Collector das ab, schreibt es in
    `SourceErrors` und misst mit den übrigen Quellen weiter — das Programm
    reißt es also nicht mit. Ihre Quelle aber wird abgeschaltet und bleibt es:
    jeder folgende Tick kehrt sofort mit demselben Fehler zurück. Sie läuft
    erst wieder an, wenn die Sensoren neu gebaut werden, also beim
    Programmstart oder nach einer Änderung an den Sensoreinstellungen. Eine
    einzige Panik kostet damit nicht einen Messwert, sondern die Quelle.
    Randfälle fangen Sie deshalb besser selbst ab.

Liefern **mehrere** Quellen denselben Wert, gewinnt die billigste. Die teure
kommt in `Collect` nach hinten und schreibt nur, was die billige nicht geliefert
hat — so springt `gpu_load` aus den WDDM-Zählern nur ein, wenn Afterburner und
NVML schweigen. Zwei Ausnahmen: wenn die teurere Quelle *genauer* misst, und
wenn zwei Quellen nur scheinbar dasselbe liefern — dann sind es zwei Messwerte
mit eigenen Bezeichnern.

### 4. Anmelden

In `internal/app/sources.go` baut `buildSensors` die optionalen Quellen
zusammen, `buildCollector` in `internal/app/app.go` übergibt sie:

```go
c.AddSource(s.sources...)
```

Ihre Quelle in diese Liste — fertig. Für Hardware, die es auf dieser Maschine
nicht gibt, hängen Sie die Quelle gar nicht erst an; in `buildSensors` steht
jeder Anhang hinter einem `if`. `AddSource` überspringt zwar ein `nil`, das
greift aber nur beim leeren Interface: ein Konstruktor mit konkretem
Rückgabetyp — `func New() *Mainboard` — liefert auch mit `return nil` ein
Interface, das nicht `nil` ist, und läuft beim ersten `Collect` in eine Panik.

## Der Vertrag, der Sie stoppen wird

Beim ersten `go test ./...` schlägt ein Test fehl, und das ist richtig so:

```
go test ./internal/metrics/ -update-catalogue
```

`internal/metrics/testdata/catalogue.txt` hält Bezeichner, Einheit, Art,
Genauigkeit, Gruppe, Panel, Kategorie und Prometheus-Namen jedes Messwerts fest.
Ändert sich davon etwas, bricht der Test. Der Befehl schreibt die Datei neu — sie
gehört mit in den Commit, und im Commit gehört gesagt, welche Konsumenten
umgehängt werden müssen.

**Eine Zeile hinzuzufügen ist die saubere Erweiterung. Umbenennen verwaist
Entitäten**, denn eine Discovery-Nachricht ist *retained*: sie liegt auf dem
Broker, überlebt das Programm und überlebt auch das Löschen der Entität von
Hand. Aufräumen heißt deshalb immer: erst den Broker leeren, dann Home
Assistant.

## Prüfen

```powershell
.\build.ps1 -Check
```

Und danach mit den Augen: `-probe` zeigt in der Konsole, welches Programm wie
viele Werte geliefert hat, das Panel **Datenquellen** auf der Anzeigeseite
dasselbe. Beide Listen entstehen aus den Werten, die da sind — eine Zeile mit
null Werten gibt es deshalb nicht: Ihre Quelle taucht nur auf, wenn sie
mindestens einen Wert geliefert hat. Und unter ihrem eigenen Namen nur, wenn
sie `collector.OriginNamer` implementiert; sonst zählt sie unter „Windows"
mit. Dass eine Quelle läuft und nichts findet, lesen Sie an der leeren Gruppe
ab und an dem Fehler, den sie in `SourceErrors` hinterlässt.

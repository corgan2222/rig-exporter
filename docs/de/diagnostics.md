# Diagnose

```bash
.\rig-exporter.exe -probe
```

Nimmt zwei Messungen im Abstand von vier Sekunden und schreibt alles heraus,
gruppiert nach Sensorgruppe, gefolgt von JSON, Prometheus-Exposition und Line
Protocol. Der schnellste Weg zu sehen, welche Quellen greifen und was bei Home
Assistant ankäme.

Auf einem Laptop ohne Afterburner oder NVIDIA-Treiber muss der Abschnitt
**Grafikkarte** im erweiterten Umfang mindestens Name, Hersteller,
Treiberversion, dedizierten und gemeinsam nutzbaren GPU-Speicher sowie
`Windows DXGI` als Datenquelle enthalten. Auslastung, Temperatur und Takt fehlen
in diesem Fall absichtlich, solange keine Live-Quelle sie wirklich misst.

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
| FPS bleibt 0, Spiel `none` | RTSS hookt die Anwendung nicht. Im RTSS-Profil „Application detection level" prüfen. Auf einer Radeon springt ohne RTSS der Treiber ein, aber nur im Vollbild — im Fenstermodus bleibt es bei 0. |
| Keine GPU-Gruppe | GPU-Gruppe in den Einstellungen aktiv? DXGI und die Windows-Geräteliste fanden keinen physischen Adapter. Ein nicht erreichbarer Afterburner betrifft nur die Live-Werte. |
| Keine CPU-Temperatur | Kommt über Afterburner, oder über PawnIO — das aber nur auf AMD und nur eleviert. |
| Keine CPU-Leistung | Gibt es ausschließlich über PawnIO: eingeschaltet, AMD, eleviert. |
| Keine Durchsatzwerte | Erst ab der zweiten Messung vorhanden, sie sind eine Differenz. |
| Entities fehlen in HA | MQTT-Integration aktiv? Discovery-Präfix identisch? Log prüfen. |

## Protokolle im Browser

Unter *Export & Anzeige* steht ganz unten der Kasten
[**Protokolle**](interface/export-and-display.md#protokolle). Er zeigt die
letzten 200 Zeilen des laufenden Protokolls, in denselben Stufen, in denen sie
geschrieben wurden:

| Stufe | Farbe |
|---|---|
| DEBUG | violett |
| INFO | weiß |
| WARN | gelb |
| ERROR | orange |
| Absturz, Panik, harter Abbruch | rot |

![Der Kasten Protokolle mit dem laufenden Log und den Dateien darunter](images/screenshots/de/export-logs.png)

*nur Fehler* blendet die ruhigen Stufen aus, ohne etwas nachzuladen. Darunter
stehen die Dateien: das laufende Protokoll, das rotierte davor und jede
aufgehobene Absturzaufzeichnung, jeweils mit Ansehen, Herunterladen und — bei
einem Absturz — dem GitHub-Knopf. Nichts davon verlässt diesen Rechner, solange
niemand einen davon drückt.

Ausgeliefert wird ausschließlich, was in dieser Liste steht. Die `config.json`
liegt im selben Ordner und wird über diesen Weg nicht herausgegeben, auch nicht
unter einem geschickt geschriebenen Namen.

## Was der Exporter selbst kostet

Eine eigene Sensorgruppe, unter der Latenzmessung, standardmäßig **aus**:

| Feld | Bedeutung |
|---|---|
| `exporter_cpu` | CPU-Anteil dieses Prozesses in %, über alle Kerne zusammen |
| `exporter_memory` | Working Set dieses Prozesses in MB |

Sie beantworten „kostet mich das Messen Frames" und „wächst der Speicherbedarf
über Tage" mit einer Zahl statt mit einer Beteuerung. Der Prozentwert nimmt
denselben Nenner wie der Task-Manager: 100 % hieße jeder Kern ausgelastet, nicht
einer. Die erste Messung nach dem Start meldet 0 %, weil eine Differenz zwei
Messungen braucht.

Aus, solange niemand fragt — zwei Werte, die fast immer flach sind, sind zwei
Entities, nach denen niemand gefragt hat, und ein Prozentwert, der den ganzen
Tag 0,0 zeigt, sieht nach einem kaputten Sensor aus statt nach einem sparsamen
Programm.

Die Werte erscheinen sofort nach dem Speichern, ohne Neustart. Beim Ausschalten
verschwinden die beiden Entities auch in Home Assistant, dafür muss HA in dem
Moment laufen.

## Top-Prozesse

Die teuerste Option des Programms, eigene Sensorgruppe, standardmäßig **aus**.
Sie beantwortet die eine Frage, die keiner der übrigen Werte beantworten kann:
der Prozessor lag bei 80 %, aber *wer* war das.

| Feld | Bedeutung |
|---|---|
| `top_cpu` | die N Programme mit dem größten CPU-Anteil, in % der ganzen Maschine |
| `top_memory` | die N Programme mit dem meisten privaten Speicher, in % des RAM |

Gruppiert wird nach Programm, nicht nach Prozess: ein Browser ist ein Eintrag,
nicht die Dutzende Prozesse, auf die er sich verteilt hat. Der Speicher zählt
**Private Bytes** statt Working Set, weil sich Working Sets nicht addieren
lassen — jeder dieser Prozesse bildet dieselben DLLs ein, und wer sie zusammen­zählt,
schreibt dem Browser Gigabytes zu, die es nur einmal gibt. Die Buchhaltungs-Töpfe `Idle`, `System`,
`Memory Compression`, `Registry` und `vmmem` fallen heraus; `Idle` würde die
CPU-Liste auf einem ruhigen Rechner sonst mit Abstand anführen.

Der CPU-Anteil bezieht sich auf die ganze Maschine, wie im Task-Manager: ein
Programm, das genau einen Thread voll auslastet, steht bei hundert geteilt durch
die Zahl der Threads — nicht bei 100. Das ist der einzige Nenner, unter dem sich
zwei Rechner mit unterschiedlicher Kernzahl überhaupt vergleichen lassen.

**Warum das teuer ist:** jede Messung liest jeden laufenden Prozess, in einem
einzigen Aufruf. Auf einem gewöhnlichen Windows sind das mehrere hundert, und
der Aufruf braucht Millisekunden statt Mikrosekunden — er kostet Rechenzeit und
blockiert, solange er läuft. Deshalb hat die Messung einen **eigenen Takt**
(Voreinstellung 10 s, Minimum 2000 ms) und hängt nicht am Auslese-Intervall: bei
einer Sekunde liefe sie dauernd und stünde jedes Mal in der Messschleife.

Der zweite Preis steht in der Datenbank von Home Assistant. Die Attribute ändern
sich bei jeder Messung, es entstehen also zwei Zeilen pro Messung — bei
10 Sekunden über 17 000 am Tag, bei 30 Sekunden ein Drittel davon. Und die Namen
der laufenden Programme stehen damit dauerhaft im Verlauf; wer den Rechner
teilt, sollte das wissen.

### Die Form: ein Sensor mit Tabelle statt fünf Entities

Jede der beiden Listen ist **eine** Entity. Ihr Zustand ist der Name des
Spitzenreiters, die vollständige Liste hängt als Attribut daran:

```yaml
sensor.re_corganpc2_top_cpu
  state: firefox.exe
  attributes:
    top: firefox.exe
    apps:
      - {name: firefox.exe, value: 41.2}
      - {name: cs2.exe,     value: 12.0}
    rank1: 41.2
    rank1_name: firefox.exe
    rank2: 12.0
    rank2_name: cs2.exe
```

Dieselben fünf Zeilen dreimal, weil drei verschiedene Leser drei verschiedene
Formen brauchen: `top` für den Zustand der Entity, `apps` zum Anzeigen einer
Tabelle, und `rank1`…`rank5` flach — denn **nur eine Zahl lässt sich zeichnen**,
eine Liste von Objekten nicht. Sind weniger Programme da als N, fehlen die
hinteren Ränge, statt als Null aufzutauchen: eine Null hieße „Programm, das
nichts verbraucht", und das ist nicht, was passiert ist.

Fünf Entities je Liste wären die Alternative gewesen — `top_cpu_1` bis
`top_cpu_5`. Dagegen spricht, dass sich das Programm hinter Platz 2 alle paar
Minuten ändert: eine Zeitreihe namens „Platz 2" zeichnet bei jedem Wechsel etwas
anderes auf und ist als Verlauf wertlos. Fünf Zeilen bleiben fünf Zeilen, wenn
sie zusammen in einem Attribut liegen.

Der Preis dieser Wahl: **keine Langzeitstatistik.** Home Assistant baut aus
Attributen keine `statistics`, und aus einem Textzustand auch nicht. Der Verlauf
lebt also nur so lange wie `purge_keep_days` (Standard 10 Tage). Die beiden
Entities stehen deshalb in der `include`-Liste des erzeugten
`recorder:`-Blocks — würde man sie ausschließen, gäbe es gar nichts zu
zeichnen.

### Nachkommastellen: hier immer, und bei der CPU zwei

Die beiden Ranglisten hängen **nicht** am Schalter *Berechne Nachkommastellen*.
Der Schalter existiert, damit sich Werte seltener ändern — was sich nicht
ändert, kostet in der Datenbank von Home Assistant keine Zeile. Eine Tabelle
gewinnt dort nichts: ihre Attribute werden bei jeder Messung ohnehin neu
geschrieben. Kosten würde die Rundung dagegen genau das, wofür die Liste da ist.

Der CPU-Anteil hat deshalb **zwei** Nachkommastellen, der Speicher eine. Ein
Anteil an der ganzen Maschine liegt bei den meisten Hintergrundprogrammen unter
einem Prozent, und je mehr Kerne ein Rechner hat, desto kleiner werden die
Zahlen. Mit einer Stelle fallen die hinteren Plätze dann alle auf denselben Wert
— das Diagramm sind gleich hohe Säulen, obwohl die Programme sich um ein
Mehrfaches unterscheiden. Die zweite Stelle trennt sie wieder.

Ein Speicheranteil wird nie so klein, dort wäre die zweite Stelle nur Rauschen.

## Bekannte Einschränkungen

Zwei Fälle, in denen ein Wert nicht fehlt, sondern falsch ist — das gehört
gesagt, bevor jemand darauf eine Automatisierung baut:

* **Hybrid-CPUs** (Intel 12. Generation und neuer, P- und E-Kerne): der
  gemeldete Takt ist dort **systematisch zu hoch**. Der Leistungsindikator
  mittelt Verhältnisse gegen je eigene Nominalfrequenzen, hier wird aber mit
  einem einzigen Basistakt multipliziert. Alle übrigen CPU-Werte stimmen.
* **Mehr als 64 logische Prozessoren:** Kernzahl und Gesamtauslastung stimmen;
  nur die optionale Liste je Kern deckt eine Prozessorgruppe ab, also höchstens
  64 davon.

## Updates

Geprüft wird direkt beim Start und danach alle **sechs Stunden**. Abschaltbar
unter [**Export & Anzeige → Anwendung**](interface/export-and-display.md#anwendung)
**→ Auf neue Versionen prüfen**; ab Werk an.
Ausgeschaltet verlässt keine Anfrage den Rechner, und es wird auch nichts
angeboten — weder in Home Assistant noch auf der Anzeigeseite.

Gibt es etwas Neueres, sind das zwei Wege zur selben Sache:

* **Auf der Anzeigeseite** erscheint ein Kasten mit der neuen Versionsnummer,
  der installierten daneben, einem Link auf die Release Notes und einem Knopf
  **Jetzt aktualisieren**.
* **In Home Assistant** kündigt MQTT eine native **Update-Entity** an, mit
  denselben Angaben und einem kurzen Auszug aus dem Changelog. Der Auszug ist
  auf die von Home Assistant vorgesehenen 255 Zeichen begrenzt; der Link führt
  deshalb immer zum ungekürzten Changelog.

Der Exporter installiert nichts unbeaufsichtigt. Erst der Klick — hier wie dort
— löst den Download aus. Währenddessen zeigt der Knopf den laufenden Vorgang an.
Danach beendet sich rig-exporter geordnet, tauscht die EXE aus und startet
wieder im Hintergrund. Die neue Instanz meldet ihre tatsächlich laufende Version
zurück, und erst dann gilt der Austausch als geglückt; meldet sie sich nicht,
wird die alte Fassung zurückgeholt.

Ersetzt wird ausschließlich die EXE, die auch **wirklich läuft**. Ein Aufruf,
der eine andere Datei austauschen wollte, wird abgelehnt.

Offizielle Update-Artefakte sind signiert. Vor dem Austausch prüft der Exporter
die Signatur der veröffentlichten Prüfsummen und anschließend die SHA-256-
Prüfsumme des Windows-Archivs. Schlägt eine dieser Prüfungen fehl, wird die
vorhandene EXE nicht ersetzt. Auch der Release-Workflow bricht ab, wenn der
Signierschlüssel fehlt oder seine Signatur nicht zum fest eingebauten
öffentlichen Zertifikat passt.


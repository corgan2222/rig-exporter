# Auslesen und Senden

![Umfang und Takte auf der Seite Messwerte](images/screenshots/de/measurements-rung.png)

Ein Takt fürs Auslesen, zwei fürs Senden:

| Einstellung | Bedeutung | Standard |
|---|---|---|
| **Auslese-Intervall** | wie oft die Hardware abgefragt wird | 500 ms |
| **Sendeintervall im Spiel** | wie oft ein Messwert die Maschine verlässt, solange ein Spiel Bilder liefert | 2000 ms |
| **Sendeintervall im Leerlauf** | dasselbe, wenn nichts gerendert wird | 10000 ms |
| **Idle-Timeout** | wie lange ein Spiel kein Bild liefern darf, bevor es als beendet gilt | 3000 ms |
| **Berechne Nachkommastellen** | ob Zahlen mit Nachkommastellen gesendet werden | an |

Die drei Intervalle liegen zwischen 250 und 300000 ms, das Idle-Timeout
zwischen 500 und 60000 ms; was daneben liegt, wird beim Speichern eingefangen.

Das Auslesen bestimmt, wie flüssig Tray und Anzeige laufen; das Senden
bestimmt, wie viel bei Broker und Zeitreihendatenbank ankommt. Wer im Tray eine
lebendige FPS-Zahl will, ohne Home Assistant zu fluten, stellt das Auslesen auf
250 ms und lässt das Senden, wo es ist.

Welcher der beiden Sendetakte gilt, wird bei jeder Messung neu entschieden und
nicht einmal beim Start: ein startendes Spiel schaltet beim nächsten Auslesen
auf den schnellen Takt, ein geschlossenes beim nächsten zurück. Als „Spiel
läuft" zählt nur, was auch Bilder liefert — RTSS hält einen Eintrag nach dem
letzten Bild noch einen Moment offen, und ein stehendes Spiel hat nichts zu
sagen, was einen schnellen Takt wert wäre.

Beide Sendeintervalle werden auf ein ganzzahliges Vielfaches des
Auslese-Intervalls aufgerundet und sind nie kürzer als dieses. Gezählt wird in
Messungen, nicht in einer zweiten Uhr — die Takte können also nicht
auseinanderdriften. Untereinander sind sie unabhängig; wer im Leerlauf häufiger
senden will als im Spiel, darf das.

Auf der Seite **Export & Anzeige** steht zwischen Home Assistant und Datenserver
der Kasten
[**Langzeitspeicherung**](interface/export-and-display.md#langzeitspeicherung-in-home-assistant),
der die dritte Stellschraube erklärt und gleich den passenden
`recorder:`-Abschnitt für diesen PC ausgibt. Der Abschnitt
wird aus den Entities gebaut, die gerade wirklich existieren: zwei Grafikkarten
ergeben zwei Temperaturzeilen, eine abgeschaltete Sensorgruppe keine. Er gehört
in die `configuration.yaml` von Home Assistant und braucht dort einen Neustart.

Wichtig dabei: ein Ausschluss nimmt einer Entity **auch** die Langzeitstatistik,
nicht nur den Verlauf. Beides zusammen oder gar nicht.

**Berechne Nachkommastellen** ist der zweite Hebel gegen eine volllaufende
Datenbank. Ausgeschaltet werden die Zahlen ganzzahlig gerechnet und gesendet —
in jedem Format, nicht nur in MQTT, damit Prometheus und Home Assistant sich
nicht über denselben Messwert uneinig sind. Ein Wert muss sich dann um eine
ganze Einheit bewegen, bevor er überhaupt als geändert zählt, und Home
Assistant schreibt nur Änderungen in seine Datenbank. Die Discovery meldet die
Genauigkeit passend mit, sonst stünde in der Oberfläche überall `x.0`.

Ausgenommen sind die beiden Ranglisten der Top-Prozesse: sie behalten ihre
Nachkommastellen, egal wie der Schalter steht. Warum, steht unter
[Nachkommastellen](diagnostics.md#nachkommastellen-hier-immer-und-bei-der-cpu-zwei).

---

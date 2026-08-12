# Oberfläche

Die Oberfläche liegt auf `http://127.0.0.1:8787` und hat vier Seiten, erreichbar
über die Kopfzeile. Grob geteilt: **Anzeige** zeigt, **Datengewinnung** bestimmt
*welche Hardware* gelesen wird, **Messwerte** bestimmt *welche Werte davon*
gesendet werden, und **Export & Anzeige** bestimmt *wohin*.

Diese Reihenfolge ist auch die sinnvolle Lesereihenfolge: eine Hardware, die auf
der zweiten Seite aus ist, taucht auf der dritten gar nicht erst auf.

| Seite | Was dort entschieden wird |
|---|---|
| [Anzeige](dashboard.md) | Nichts — hier wird gezeigt. Bis auf zwei Schalter, die man genau hier braucht |
| [Datengewinnung](data-capture.md) | Welche Hardware überhaupt gelesen wird |
| [Messwerte](measurements.md) | Welche der gelesenen Werte gesendet werden, und wie oft |
| [Export & Anzeige](export-and-display.md) | Die Felder für MQTT, Datenserver und InfluxDB — und wie sich das Programm selbst verhält |

Dazu zwei Seiten, die für alle vier gelten:
[Was für alle Seiten gilt](common.md) und
[Oberfläche im Netzwerk](on-the-network.md). Zwei weitere hängen an ihnen und
stehen eine Ebene höher, weil sie umfangreich genug sind:
[Auslesen und Senden](../polling-and-publishing.md) und das
[Tray-Menü](../tray-menu.md).

Was auf der anderen Seite ankommt — Topics, Endpunkte, die Konfiguration des
Empfängers —, steht unter [Exportziele](../export-targets.md).

![Die Anzeigeseite, auf der die Oberfläche startet](../../images/screenshots/de/dashboard.png)

# Erster Start

1. `rig-exporter.exe` starten — ein Tacho-Symbol erscheint im Infobereich, und
   die Oberfläche öffnet sich im Standardbrowser.
2. Exportziel und Sensorgruppen wählen, **Speichern & übernehmen**.

Konfiguration und Log liegen in `%APPDATA%\rig-exporter`.

Der Browser öffnet sich **nur beim Start von Hand**. Der Autostart-Eintrag
trägt `-background`, und damit bleibt es beim Tray-Symbol: ein ungefragtes
Browserfenster bei jeder Anmeldung ist der schnellste Weg, den Autostart wieder
abzuschalten. Wer das Verhalten nachstellen will, startet selbst mit
`-background`.

Vier Schalter gibt es auf der Kommandozeile, alles Weitere steht in der
Oberfläche:

| Schalter | Wirkung |
|---|---|
| `-version` | gibt Name und Version aus und beendet sich |
| `-probe` | nimmt eine Messung, gibt sie in allen Formaten aus, beendet sich |
| `-background` | startet ohne den Browser zu öffnen |
| `-config <pfad>` | benutzt diese Datei statt `%APPDATA%\rig-exporter\config.json` |

`-version` und `-probe` laufen, bevor geprüft wird, ob schon eine Instanz läuft
— sie gehen also auch neben einem laufenden Exporter. Ein normaler Start tut das
nicht: eine zweite Instanz meldet sich mit einem Hinweis und beendet sich.

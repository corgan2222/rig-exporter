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

Fünf Schalter gibt es auf der Kommandozeile, alles Weitere steht in der
Oberfläche:

| Schalter | Wirkung |
|---|---|
| `-version` | gibt Name und Version aus und beendet sich |
| `-probe` | nimmt eine Messung, gibt sie in allen Formaten aus, beendet sich |
| `-background` | startet ohne den Browser zu öffnen |
| `-config <pfad>` | benutzt diese Datei statt `%APPDATA%\rig-exporter\config.json` |
| `-demomode` | ergänzt das Tray-Menü um ein simuliertes Spiel — siehe unten |

`-version` und `-probe` laufen, bevor geprüft wird, ob schon eine Instanz läuft
— sie gehen also auch neben einem laufenden Exporter. Ein normaler Start tut das
nicht: eine zweite Instanz meldet sich mit einem Hinweis und beendet sich.

### Demo-Modus

`-demomode` ist dafür da, ein Dashboard zu bauen und vorzuführen, ohne dass ein
Spiel läuft. Der Schalter ergänzt das Tray-Menü um drei Einträge, und solange
keiner davon benutzt wird, ändert sich sonst nichts:

| Eintrag | Wirkung |
|---|---|
| **Demo: simulate a game** | schaltet die Simulation ein und aus |
| **Demo: game** | Red Dead Redemption 2, Grand Theft Auto V oder Cyberpunk 2077 |
| **Demo: store** | von welchem Launcher das Spiel angeblich stammt: `steam`, `gog` oder `epic` |

Die Einträge sind englisch und werden nicht übersetzt.

Eingeschaltet tritt das simulierte Spiel an die Stelle dessen, was RTSS meldet:
eine Bildrate, die um einen für den gewählten Titel typischen Wert wandert, die
Exe als laufendes Spiel, und — auch bei abgeschalteter
[Spielerkennung](game-identification.md) — Plattform, Titel und die echte
Steam-App-ID, damit das Artwork aufgelöst wird.

!!! warning "Diese Werte sind nicht gemessen"
    Sie gehen den gewöhnlichen Weg in jedes eingeschaltete Exportziel, MQTT und
    InfluxDB eingeschlossen, und landen dort im Verlauf wie echte. Einen
    Test-Broker benutzen, oder mit einem Spiel in der Statistik rechnen, das nie
    lief.

Ohne den Schalter gibt es die Menüeinträge nicht, und die Simulation ist gar
nicht Teil des laufenden Programms.

# Anzeige

![Die Anzeigeseite mit Kacheln, Zustandsschaltern und einem Panel je Hardware](../../images/screenshots/de/dashboard.png)

Live-Werte, Zustand der Exportziele, ein Panel je Sensorgruppe und die Adressen
der aktiven Endpunkte. Aktualisiert sich im Auslese-Intervall. Die einzige Seite
ohne Einstellungen — bis auf zwei Schalter, die hier stehen, weil man hier
merkt, dass man sie braucht.

Unter den Kacheln sagen vier Chips, was gerade eingestellt ist: welcher Umfang,
ob mit Nachkommastellen, wie viele Entities entstehen, und in welchem Takt
gesendet wird — dabei der Takt, der **gerade** gilt, mit dem Zusatz „im Spiel"
oder „Leerlauf". Eine Zahl ohne diesen Zusatz wäre wertlos, weil es zwei davon
gibt. Jeder Chip führt auf die Einstellung, die ihn bestimmt; einen Wert zu
lesen und ihn zu ändern soll nicht zwei Suchen sein. Kacheln für Werte, die der
gewählte Umfang gar nicht misst, werden ausgeblendet statt leer angezeigt.

In den Hardware-Panels lässt sich zwischen zwei Ansichten umschalten.
Voreingestellt ist **nach Gerät**: alles zu GPU 0 zusammen, dann alles zu GPU 1,
jede Platte für sich, jeder Adapter für sich — jede Gruppe unter einer
Überschrift mit dem Gerätenamen, die Werte darunter nur kurz benannt. **Nach
Messwert** lässt diese Überschriften weg und schreibt das Gerät stattdessen in
jede einzelne Zeile. Die Reihenfolge ist in beiden Fällen dieselbe, Gerät für
Gerät; es ändert sich nur, wo der Gerätename steht. Die Wahl bleibt im Browser
gespeichert.

Fehlt auf einem Rechner die GPU beziehungsweise werden dort bewusst keine
Spieldaten genutzt, lässt sich der RTSS-Hinweis dauerhaft wegräumen. Der Knopf
heißt **„Keine GPU vorhanden — Spieldaten ausblenden"**, wenn Windows gar keine
Grafikkarte meldet, und **„Kein Spielrechner — Spieldaten ausblenden"**, wenn
eine da ist: derselbe Schalter, aber einer Radeon zu erklären, sie sei nicht
vorhanden, wäre schlicht falsch. Die Einstellung wird als `no_gpu` in
`config.json` gespeichert und blendet zusätzlich die Kacheln FPS, Frametime und
Spiel sowie den RTSS-Statuschip aus. Unter
[*Export & Anzeige → Anwendung*](export-and-display.md#anwendung) lässt sie sich
wieder abschalten. Messung und Exporte ändern sich dadurch nicht.


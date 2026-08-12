# Bezeichner und Einordnung

Wie ein Messwert heißt, und wo Home Assistant ihn hinlegt.

## Wie die Bezeichner aufgebaut sind

Ein Bezeichner nennt erst das **Gerät**, dann die **Messgröße**:

```
diskc_used_percent      gpu0_temperature      net_ethernet_2_rx
diskd_free              gpu1_vram_used        net_ethernet_2_link
```

So stehen alle Werte eines Laufwerks beieinander. Gerät und Nummer wachsen zu
einem Wort zusammen — `gpu0`, `diskc` liest man als eine Einheit. Nur wenn die
Instanz selbst mehrteilig ist, bleibt das Trennzeichen: `netethernet_2` wäre
unlesbar.

In Home Assistant kommt davor, wer den Wert liefert und von welchem Rechner:

```
sensor.re_corganpc2_gpu0_vendor
sensor.re_corganpc2_diskc_free
```

`re` steht für rig-exporter. Von links nach rechts beantwortet die Kennung damit
genau die Fragen in der Reihenfolge, in der man sie beim Überfliegen einer Liste
von hundert Entitäten stellt: welches Programm, welcher Rechner, welche
Hardware, welcher Messwert.

Nicht überall ist das richtig. Ein Prozessorkern wird durch das Wort `cpu_core`
gezählt, `cpu_core_5` liest sich also bereits korrekt — `cpu_5_core` wäre
Unsinn. Dasselbe gilt für Speichermodule. Umgestellt wurden die vier
Dimensionen, bei denen das Gerät vor der Messgröße gehört: Grafikkarten,
Laufwerke, Netzwerkadapter und Kühlungssteuerungen.

**Ändert sich eine Schreibweise doch einmal**, räumt das Programm die frühere
beim nächsten Verbinden selbst vom Broker ab — die Entität verschwindet also aus
Home Assistant, statt als „nicht verfügbar" stehen zu bleiben. Dashboards und
Automatisierungen, die auf den alten Namen zeigen, muss man dann umhängen.

Das ist nötig, weil eine Discovery-Nachricht **retained** ist: sie liegt auf dem
Broker und überlebt sowohl dieses Programm als auch das Löschen der Entität von
Hand — die kommt beim nächsten Neustart von Home Assistant einfach wieder.
Deshalb wird jeder alte Name ausdrücklich mit einer leeren Nachricht
zurückgenommen. In ein Topic zu schreiben, das es nie gab, tut nichts, also
braucht das kein Migrationsflag und kein Gedächtnis.

## Wo Home Assistant die Werte einsortiert

67 Messwerte stehen im Hauptbereich, 48 unter **Diagnose**, 7 werden gar nicht
als Entität veröffentlicht. Gezählt über den vollen Katalog — was auf einem
bestimmten Rechner ankommt, hängt an seiner Hardware und an der Auswahl.
Die Regel dahinter:

* **Diagnose** — Tatsachen *über* die Maschine statt Messungen *an* ihr: Modell,
  Hersteller, Dateisystem, Kapazität, Steckplätze, Nenn- und Grenzwerte,
  Windows-Version. Alles, was man beim Fehlersuchen ansieht und was sich nicht
  von selbst bewegt. Home Assistant hält das aus der Hauptliste und aus
  automatisch erzeugten Dashboards heraus.
* **Hauptbereich** — was der Rechner gerade tut: Bilder pro Sekunde,
  Temperaturen, Auslastung, freier Platz, Durchsatz, Leistung.

Die Grenzfälle entscheidet der Verwendungszweck, nicht die Form. Der
Anzeigemodus ist im Prinzip Konfiguration, aber eine still auf 60 Hz gefallene
Bildwiederholrate ist genau das, was auf ein Dashboard gehört — also
Hauptbereich. Leerlaufzeit treibt Anwesenheits-Automatisierungen und bleibt
ebenfalls oben, während Laufzeit die Frage „wie lange seit dem letzten
Neustart" beantwortet und Diagnose ist. Gleiche Form, andere Aufgabe.

Festgeschrieben ist die Einordnung in `testdata/catalogue.txt`: einen Wert
umzusortieren verschiebt ihn in Home Assistant aus der Hauptliste heraus, und
das soll im Review auffallen statt beim Nutzer.

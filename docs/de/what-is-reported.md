# Was gemeldet wird

Es wird nur gemeldet, was der Rechner tatsächlich liefert. Fehlt die Quelle
einer Gruppe, entstehen dafür gar keine Entities — und sie erscheinen von
selbst, sobald die Quelle da ist. Jede Gruppe lässt sich einzeln abschalten.

Eine Quelle, die nicht rechtzeitig antwortet, wird für diesen Takt übergangen.
Die übrigen Messwerte gehen pünktlich hinaus; die ausgefallene Gruppe nennt den
Grund auf der Statusseite. Dasselbe gilt, wenn eine Quelle abstürzt: sie wird
abgeschaltet statt das Programm mitzunehmen, und im Protokoll steht, warum.

Quer über alle Gruppen liegt der **Umfang**. Die Gruppen sagen, welche Hardware
gelesen wird; der Umfang sagt, wie ausführlich. Ein Schieberegler auf der Seite
**Messwerte** kennt drei Stufen:

* **Minimal** — 16 Messwerte: die Kacheln der Anzeigeseite. Prozessor- und
  Grafiktemperatur, Gesamtbelegung, Durchsatz, Akku. Bewusst ohne Laufzeit und
  Prozessanzahl, denn die ändern sich bei jeder Messung und füllen eine
  Datenbank mit nichts.
* **Standard** — 76 Messwerte: was man sich ansieht, wenn man wissen will, wie
  es dem Rechner geht. Temperatur, Auslastung, freier Platz, Durchsatz, FPS.
* **Erweitert** (Voreinstellung) — alle 122: Taktraten, Speicher­riegel,
  Last je Thread, Anzeigemodus, Zustand von RTSS, Akkuverschleiß. Nützlich beim
  Suchen eines Problems, im Alltag selten.

Die Zahlen sind die Obergrenze der jeweiligen Stufe, nicht das, was am Ende
ankommt. Wie viele Messwerte ein Rechner wirklich meldet, entscheidet er selbst
und Sie: eine Maschine ohne Akku hat keine Akku-Werte, eine ohne
USB-Kühlungssteuerung keine Kühlungswerte, und jeder einzelne Messwert lässt
sich abwählen. Auf einem Desktop ohne Akku und ohne AIO sind es rund zwanzig
weniger als hier steht.

Der Regler setzt nur die Grundstellung. Darunter steht **jeder Messwert einzeln
zum Abhaken**, nach Knoten sortiert, mit dem Wert, den er gerade liest — man
sieht also, was man einschaltet, bevor man es einschaltet. Gespeichert wird die
Stufe plus die Abweichungen davon, nicht die Liste: ein Messwert, den eine
spätere Version dazubaut, kommt dadurch von selbst mit.

Daneben steht eine **Überschlagsrechnung**: wie viele Entities die Auswahl auf
diesem PC ergibt, wie viele Datenbankzeilen das pro Tag sind und wie groß die
Datenbank damit wird. Sie rechnet nicht mit geratenen Zahlen — der Anteil
tatsächlich wechselnder Werte wird laufend gemessen, und die zwei angenommenen
Größen (300 Byte je Zeile, 10 Tage Aufbewahrung) stehen daneben. Die Sende­takte
liegen mit auf derselben Seite, weil sie die Zeilenzahl genauso bestimmen wie
die Auswahl; die Anzeige bewegt sich beim Tippen mit. Nichts davon braucht einen
Speichern-Knopf: jede Änderung wirkt sofort.

Die Auswahl wirkt überall gleich: was nicht angehakt ist, entsteht gar nicht
erst und fehlt deshalb in MQTT, JSON, Prometheus, InfluxDB **und** auf der
eigenen Anzeigeseite. Ein Wert, den das Dashboard zeigt und Home Assistant nie
bekommt, wäre die schlechtere Einstellung.

Beim Abwählen werden die abgewählten Entities in Home
Assistant **entfernt**, nicht bloß nicht mehr beliefert: für jede geht eine leere
retained Nachricht auf ihr Discovery-Topic, und genau das ist der Löschbefehl.
Der Weg zurück kündigt sie wieder an. Home Assistant behält dabei einen
Grabstein mit Bereich, Name, Symbol und Beschriftungen, sodass eigene Anpassungen
den Rundweg überleben; der aufgezeichnete Verlauf bleibt ohnehin unberührt.

> **Beim Umschalten sollte Home Assistant laufen.** Die Löschung ist eine
> Nachricht, kein Zustand — wer nicht zuhört, verpasst sie. Und weil die leere
> Nachricht die alte zugleich vom Broker nimmt, findet ein später startendes
> Home Assistant nichts mehr vor und holt die Entities aus seiner eigenen
> Registry als dauerhaft *nicht verfügbar* zurück. Läuft es mit, verschwinden
> sie sauber.

Das Entfernen passiert **nur** bei dieser einen, ausdrücklichen Änderung. Eine
abgeschaltete Sensorgruppe oder Hardware, die gerade nicht antwortet, lässt ihre
Entities stehen — die kommen wieder, und eine Entity, die zwischendurch entfernt
wurde, hätte ihren Verlauf für nichts verloren.

| Gruppe | Werte | Quelle |
|---|---|---|
| **FPS & System** (immer an) | FPS, Frametime, laufendes Spiel, Auflösung, Bildwiederholrate, CPU-Last, RAM-Last, Windows-Version, virtuelle Maschine und Hypervisor, Anzahl Prozesse, Laufzeit, Leerlaufzeit | RTSS + Windows; FPS ersatzweise aus dem AMD-Treiber |
| **Grafikkarte** | Name, Hersteller, Treiberversion, dedizierter und gemeinsam nutzbarer Speicher, Temperatur, Hotspot, Kern- und Speichertakt, Auslastung und ihre Aufteilung auf 3D, Videodekodierung, Videokodierung und Kopier-Engine, belegter Grafikspeicher, VRAM, Lüfter (% und U/min), Leistung, Leistungsgrenze und deren Ausschöpfung, Spannung — pro Karte | Windows DXGI, Plug and Play und die WDDM-Leistungsindikatoren, MSI Afterburner, NVML (NVIDIA) und ADLX (AMD) |
| **Prozessor** | Modell, Hersteller, Kerne, Threads, Basis-, wirksamer und höchster beobachteter Takt, Temperatur, Leistung, Load über 1/5/15 Minuten, optional Last je Thread | Windows, Temperatur über Afterburner oder PawnIO, Leistung nur über PawnIO (AMD, eleviert) |
| **Arbeitsspeicher** | belegt und frei in MB, frei in %, gesamt, Takt, maximaler Takt, Typ, bestückte und vorhandene Steckplätze, ein Eintrag je Modul | Windows + SMBIOS der Firmware |
| **Laufwerke** | Typ (NVMe/SSD/HDD), Label, Dateisystem, Hersteller, Kapazität, belegt, frei, Belegung und freier Anteil in %, Lesen, Schreiben, Auslastung — pro Volume, dazu fünf Summenwerte über alle | Windows |
| **Netzwerk** | Adapter, Link-Speed, Download- und Upload-Rate, empfangene und gesendete Gesamtmenge, Fehler, verworfene Pakete, WLAN-Signal, Ping und Paketverlust | Windows + ICMP |
| **Akku** | Ladestand, Netzbetrieb, Laden, Restenergie, Lade- bzw. Entladeleistung, Restlaufzeit; im erweiterten Satz Zustand, Ladezyklen, Design- und Ladekapazität, Chemie, Spannung | Windows Power-API + Akkugerät |
| **Kühlung** | Modell, Flüssigkeitstemperatur, Pumpen- und Lüfterdrehzahl; im erweiterten Satz die Regelung von Pumpe und Lüfter in Prozent | USB-Kühlungssteuerung (HID) |
| **Eigene Ressourcennutzung** | CPU-Anteil und Speicherbedarf von rig-exporter selbst | Windows |
| **Top-Prozesse** | die Programme mit dem größten CPU- und Speicherbedarf | Windows |

Wie viele Werte daraus werden, entscheidet die Hardware: jede Grafikkarte, jedes
Laufwerk und jeder Adapter bringt seinen eigenen Satz mit. Fest ist die Zahl
auch dann nicht — einem Laufwerk ohne auslesbaren Hersteller fehlt dieser eine
Wert, und niemand erfindet ihn.

CPU- und RAM-Last gehören zu **FPS & System**, damit sie unabhängig von jedem
Schalter da sind — die Kacheln oben auf der Seite brauchen sie. Angezeigt werden
sie trotzdem bei Prozessor und Arbeitsspeicher, weil dort danach gesucht wird.
Ein zweiter Sensor mit demselben Wert wäre die Alternative, und zwei
Home-Assistant-Entitäten für dieselbe Zahl sind schlechter als keine.

## Woher die Grafikwerte kommen

Windows kennt jede Grafikkarte selbst: DXGI liefert Modell, PCI-Hersteller,
dedizierten Grafikspeicher und die Obergrenze des gemeinsam nutzbaren
Systemspeichers; Plug and Play ergänzt die installierte Treiberversion.
Temperatur, Takt und Leistung gehören dagegen nicht zu diesen Schnittstellen.
Deshalb greifen fünf Quellen ineinander:

1. **Windows DXGI** (`CreateDXGIFactory1` / `EnumAdapters1`) bildet das Inventar.
   Es braucht weder Zusatzsoftware noch Administratorrechte und erkennt damit
   auch eine integrierte Intel Iris auf einem normalen Laptop.
2. **Die WDDM-Leistungsindikatoren** (`GPU Engine`, `GPU Adapter Memory`) liefern
   Auslastung und belegten Grafikspeicher — dieselben Zahlen, die der
   Task-Manager auf seiner GPU-Seite zeichnet. Sie kommen aus dem
   Windows-Grafikkern, nicht aus einem Herstellertreiber, brauchen keine
   Rechte und funktionieren auf Intel, AMD und NVIDIA gleich.
3. **MSI Afterburner** (`MAHMSharedMemory`) liefert die Live-Werte: deckt NVIDIA,
   AMD und Intel ab, liefert Lüfter, Spannung und Hotspot. RTSS gehört ohnehin
   dazu, ein für den FPS-Overlay eingerichteter Rechner hat das also schon.
4. **NVML** aus dem NVIDIA-Treiber füllt die Lücken, vor allem den
   VRAM-Gesamtausbau und die Lüfterdrehzahl. Ohne Afterburner reicht es allein
   für NVIDIA-Karten.
5. **ADLX** (`amdadlx64.dll`) ist das Gegenstück auf der AMD-Seite und kommt mit
   dem Adrenalin-Treiber. Es liefert Temperatur, Hotspot, Kern- und
   Speichertakt, Leistung, Lüfterdrehzahl, Spannung und den VRAM-Ausbau, und
   reicht damit ohne Afterburner für Radeon-Karten. Der reine Anzeigetreiber
   bringt es nicht mit — dafür braucht es das vollständige Paket.

Die Leistungsindikatoren geben die Auslastung nach **Engine** aufgeschlüsselt:
3D, Videodekodierung, Videokodierung und Kopier-Engine, jede als Summe über alle
Prozesse. Zusammengezählt werden sie nicht — drei Engines zu je 60 % ergäben
180 %, und mehr als voll beschäftigt kann eine Karte nicht sein. Der
Gesamtwert ist deshalb die **belegteste** Engine, wie im Task-Manager.

`gpu_load` wird daraus nur gefüllt, wenn weder Afterburner noch NVML ihn
geliefert haben. Auf einem Rechner mit NVIDIA-Karte ändert sich also nichts; auf
einem Laptop mit reiner Intel-Grafik gibt es damit zum ersten Mal überhaupt eine
GPU-Auslastung. **ADLX ist hier bewusst nicht beteiligt:** eine Herstellerquelle
darf den Zählern einen Wert nur abnehmen, wenn sie ihn genauer misst, und
ADLX' `GPUUsage` ist eine Momentaufnahme — auf einer RX 570 meldete sie 1 %,
während der 3D-Zähler bei 39,6 % stand. Auf AMD bleibt `gpu_load` deshalb bei
den Leistungsindikatoren. Der belegte Grafikspeicher bekommt dagegen einen **eigenen**
Bezeichner (`gpu_memory_used` neben `gpu_vram_used`): das eine ist, was der
Grafikkern vergeben hat, das andere, was die Karte selbst meldet. Die Zahlen
gehen auseinander, und ein Wert, der seine Bedeutung mit der Quelle wechselt,
ist schlechter als zwei Werte, die je eine Sache bedeuten.

Nicht jede Engine wird gemeldet. VR, OFA, Security, JPEG-Dekodierung und das
Legacy-Overlay stehen auf gewöhnlicher Hardware dauerhaft auf null und wären
fünf Entities, die nie etwas sagen.

Die Zähler werden alle fünf Sekunden gelesen, nicht im normalen Messtakt: sie
liefern eine Zeile je Prozess, Adapter und Engine, was auf einem normalen
Rechner mehrere hundert sind. Der Wert ist der Durchschnitt über dieses Fenster,
ein längeres Fenster also kein gröberer Messwert, sondern ein ruhigerer.

Auf einer NVIDIA-Karte fehlen ohne Afterburner nur die Werte, die NVML nicht
kennt, etwa Hotspot und Spannung. Auf einer Radeon deckt ADLX inzwischen
dasselbe ab; offen bleiben dort die Leistungsgrenze, die ADLX überhaupt nicht
führt, und der Lüfter in Prozent — ADLX kennt nur die Drehzahl, und die
Drehzahl durch ihren Höchstwert zu teilen wäre eine andere Größe unter
demselben Bezeichner. Auf Intel bleibt ohne Live-Quelle das DXGI-Inventar
sichtbar; nicht messbare Werte werden weggelassen statt als null behauptet.
Welche Sensoren eine Karte hat, entscheidet sie selbst: eine Polaris-Radeon
antwortet auf Hotspot und Spannung mit `ADLX_NOT_SUPPORTED`, weil sie beide
Sensoren nicht besitzt. NVML meldet auch die Lüfterdrehzahl (`nvmlDeviceGetFanSpeedRPM`,
gemeldet wird der schnellste Lüfter der Karte) und wächst mit jeder
Treibergeneration um neue Einsprungpunkte, und `LazyProc.Call` löst das Symbol
über `mustFind` auf — das **panict**, wenn es fehlt. In einem Binary mit
`-H windowsgui` stirbt damit das Tray wortlos. Deshalb wird jeder Einsprungpunkt
einmal aufgelöst und vor dem ersten Aufruf geprüft; ein alter Treiber verliert
einen Wert, nicht das Programm. Für ADLX gilt dasselbe: von dort werden
ohnehin nur zwei Symbole gebraucht, alles Weitere läuft über
Funktionszeigertabellen wie bei DXGI, und beide werden vor dem ersten Aufruf
geprüft.

Ohne Afterburner und ohne Herstellerquelle entfallen also nur Temperatur, Takt,
Lüfter und Leistung — Inventar, Auslastung und Speicherbelegung bleiben. Ohne
Kernel-Treiber sind Gehäuselüfter, Netzteil-Telemetrie und Spannungen
grundsätzlich nicht erreichbar.

Die Quellen zählen unabhängig voneinander durch. DXGI legt die Instanzen
fest, Afterburner, NVML und ADLX werden über den Kartennamen darauf abgebildet — der
ist allerdings nicht eindeutig, zwei gleiche Karten heißen gleich. Zugeordnet
wird darum in Indexreihenfolge und jede Instanz höchstens einmal. Zusätzlich
begrenzt die Plug-and-Play-Geräteliste, wie oft dieselbe PCI-Kennung vorkommen
darf: Ein Citrix-Sitzungsadapter kann sonst eine echte Karte in DXGI spiegeln,
während zwei wirklich eingebaute gleiche Karten erhalten bleiben. Auf einem
Hybrid-Notebook vertauscht eine abweichende Aufzählungsreihenfolge dadurch
nicht mehr Intel- und NVIDIA-Werte.

## Alle Laufwerke zusammen

Neben den Werten je Volume gibt es fünf Summen über alle gemeldeten Laufwerke:

| Feld | Bedeutung |
|---|---|
| `disk_overall_capacity` | Kapazität aller Laufwerke zusammen, in GB |
| `disk_overall_used` | davon belegt, in GB |
| `disk_overall_free` | davon frei, in GB |
| `disk_overall_usage` | belegter Anteil in % |
| `disk_overall_free_percent` | freier Anteil in % |

In Home Assistant heißen sie **„Laufwerke Gesamtkapazität"**, „Laufwerke Gesamt
belegt" und so weiter — plural, während ein einzelnes Volume „Laufwerk C: Frei"
heißt. Der Unterschied ist Absicht: die Summen beschreiben kein bestimmtes
Laufwerk.

„Wie voll ist dieser Rechner" ist die Frage, die vor jeder Frage nach einem
einzelnen Laufwerk kommt, und sie aus vier Entities in einem Template
zusammenzurechnen ist Arbeit, die niemand zweimal machen will. Die Summen
gehören deshalb schon zur Stufe **Standard**.

Summiert wird genau das, was auch gemeldet wird: ein über **Nur diese
Laufwerke** ausgeschlossenes Volume zählt nicht mit, und eines, das sich nicht
lesen ließ, ebenso wenig. Die Summe ist damit immer die Summe dessen, was in
der Liste steht. Gerechnet wird in Bytes und erst am Ende gerundet — sie ist
also genauer als die Summe der gerundeten Einzelwerte.

**Was gar nicht erst als Laufwerk zählt:** Netzlaufwerke, optische Laufwerke,
Wechselmedien — und alles, was an einem USB-Anschluss hängt. Der Laufwerkstyp
allein reicht dafür nicht: ein USB-Stick meldet sich als Wechselmedium und
fällt schon dort heraus, eine externe USB-SSD meldet sich dagegen fast immer
als fest eingebaut und ist ohne Nachfrage beim Speicher-Stack nicht von einer
internen Platte zu unterscheiden. Gefragt wird deshalb nach dem Bus. Eine
Sicherungsplatte, die heute zufällig ansteckt, würde die Gesamtzahlen sonst
springen lassen, ohne dass sich am Rechner etwas geändert hat. Antwortet ein
Laufwerk nicht auf die Frage, bleibt es drin — nicht wissen ist kein Grund,
etwas wegzulassen.

## Der aktive Netzwerkadapter

Standardmäßig wird nur der Adapter gemeldet, über den die Default-Route läuft.
Ein Rechner mit Hyper-V, WSL, VPN und Capture-Treiber hat sonst schnell ein
Dutzend Interfaces, und das eine, das zählt, geht darin unter. Umschaltbar über
**Alle Adapter statt nur dem aktiven**.

Fällt die Default-Route weg — Kabel gezogen, WLAN abgerissen —, wird weiter der
zuletzt aktive Adapter gemeldet, nicht auf alle umgeschaltet. Das ist Absicht:
virtuelle Adapter gehen nicht mit herunter, wenn die physische Karte ausfällt.
Auf einer Maschine mit sechs Hyper-V-Switches, Tailscale und ZeroTier hätten
fünf Sekunden ohne Kabel neunzig Entities angelegt — jede mit einer *retained*
Discovery-Nachricht, die den Ausfall überlebt. Gibt es noch gar keinen zuletzt
aktiven Adapter, meldet die Gruppe für diesen Takt nichts und nennt den Grund
auf der Anzeigeseite.

**Was trotzdem bleiben kann.** Wechselt der Rechner den aktiven Adapter dauerhaft
— beim Andocken von WLAN auf Ethernet, oder wenn ein VPN die Route übernimmt —,
entsteht für den neuen eine Entity, und die alte bleibt. Das ist gewollt: eine
externe Platte, die einen Nachmittag abgezogen ist, soll niemandem seinen
Verlauf kosten, und dieselbe Regel gilt für Adapter. Wer aufräumen will, geht in
dieser Reihenfolge vor — sonst kommt die Entity beim nächsten Start zurück:

1. die retained Discovery auf dem Broker löschen (leere Nachricht auf dasselbe
   Topic, z. B. mit `mosquitto_pub -r -n -t <topic>`),
2. danach die Entity in Home Assistant entfernen.

Ping und Paketverlust laufen in einem eigenen Takt, unabhängig vom
Sendeintervall: eine Runde gegen einen nicht erreichbaren Host dauert Sekunden
und darf die Messschleife nicht blockieren. Ziel ist standardmäßig das
Default-Gateway.

**Rate und Menge sind zwei verschiedene Werte.** Pro Adapter gibt es beides:

| Feld | Anzeige | Bedeutung |
|---|---|---|
| `net_rx` | Download | aktuelle Rate in Mbit/s |
| `net_tx` | Upload | aktuelle Rate in Mbit/s |
| `net_rx_total` | Empfangen gesamt | Datenmenge in GB, seit der Adapter oben ist |
| `net_tx_total` | Gesendet gesamt | Datenmenge in GB, seit der Adapter oben ist |

Die Raten heißen **Download** und **Upload**, nicht „Empfangen" und „Gesendet" —
das liest sich auf einer Geräteseite wie eine Summe, obwohl Mbit/s eine
Geschwindigkeit ist.

Die Summen sind die Zähler, die Windows je Interface führt, nicht eine
zurückgerechnete Rate. Das ist der Unterschied zwischen „gemessen" und
„geschätzt": eine Riemann-Summe über eine 2-Sekunden-Reihe verliert jeden
Verkehr, der zwischen zwei Messungen lag. Sie tragen `state_class:
total_increasing` — damit weiß Home Assistant, dass ein Rückfall auf 0 ein
Zählerneustart ist und kein negativer Verkehr. Windows setzt diese Zähler
nämlich zurück, wenn ein Adapter neu konfiguriert wird.

GB heißt hier 2³⁰ Byte, wie überall sonst im Katalog und wie im Windows
Explorer. Deshalb tragen die beiden bewusst **keine** `device_class`: Home
Assistant würde `data_size` als 10⁹ lesen und beim Umrechnen von der falschen
Grundlage ausgehen.

## Spezielle Hardware — AIO-Kühlung

Wasserkühlungen, Pumpen und Lüfter-Hubs hängen als USB-Gerät am Rechner und
melden sich als HID an. Sie schicken ihren Zustand von sich aus, etwa im
Sekundentakt — es genügt zuzuhören. Kein Herstellerprogramm, kein Treiber, keine
Adminrechte, und die Software des Herstellers kann nebenher weiterlaufen.

**Es wird ausschließlich gelesen.** An keiner Pumpenkurve und keinem
Lüfterprofil wird etwas verstellt; das Programm schreibt nicht ein Byte an ein
solches Gerät.

Die Quelle heißt in der Oberfläche **Spezielle Hardware** und ist als
`ALPHA · ungetestet` gekennzeichnet. Das ist wörtlich gemeint: die Protokolle
sind von keinem Hersteller veröffentlicht, sie stammen aus
[LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/tree/master/LibreHardwareMonitorLib/Hardware/Controller),
und geprüft wurde bisher **eine** Familie an echter Hardware.

| Gerät | USB-Kennung | Stand |
|---|---|---|
| NZXT Kraken Z3 | `1E71:3008` | an echter Hardware geprüft |
| NZXT Kraken X3 | `1E71:2007`, `1E71:2014` | gleiches Protokoll, ungeprüft |
| NZXT Kraken 2023 / Elite | `1E71:300C`, `1E71:300E` | gleiches Protokoll, ungeprüft |
| NZXT Kraken Elite V2 | `1E71:3012` | gleiches Protokoll, ungeprüft |

Findet sich kein passendes Gerät, entsteht keine einzige Entity — wie bei jeder
anderen Gruppe auch. Auf der Anzeigeseite und in der Messwertauswahl taucht der
Kasten dann gar nicht erst auf.

## Wenn es abstürzt

Ein Programm, das mit `-H windowsgui` gebaut ist, startet ohne aufblitzende
Konsole — und hat dafür **kein stderr**. Genau dorthin schreibt Go eine Panik
samt Stapelspur. Ohne Gegenmaßnahme verschwindet also ausgerechnet das, was den
Fehler erklärt: der Prozess ist weg, im Log steht nichts, und im
Windows-Ereignisprotokoll steht auch nichts, weil die Go-Laufzeit den Fehler
selbst behandelt.

rig-exporter gibt sich deshalb beim Start ein stderr zurück und zeigt auf
`crash.log` neben der Konfiguration. Was die Laufzeit im Ernstfall schreibt,
landet damit auf der Platte statt im Nichts — auch die Panik einer Goroutine,
die kein `recover` je auffangen könnte.

Eine Sitzung, die sich ordentlich beendet, leert die Datei wieder — und zwar
jede geplante Beendigung, nicht nur die über das Tray. Ein Update, das an seinen
Helfer übergibt, und ein Startabbruch mit Fehlerdialog hinterlassen deshalb
keinen Absturzbericht. Ist beim nächsten Start etwas darin, war der letzte Lauf
keiner:

| Inhalt | Bedeutung |
|---|---|
| leer | sauber beendet |
| nur die Kopfzeile | hart beendet — Task-Manager, Stromausfall, überschriebene EXE |
| Panik samt Stapelspur | ein Fehler im Programm |

Der Bericht wird zur Seite gelegt, das Log bekommt eine Fehlerzeile, und auf der
Anzeigeseite steht ein Kasten ganz oben. Aufgehoben wird unter dem Namen

```
rig-exporter_<rechner>_crashreport_<datum>_<uhrzeit>.log
```

— die letzten zehn bleiben liegen. Der Rechnername steht darin, weil solche
Dateien wandern: an ein Issue gehängt, in einen Chat gezogen, per Mail
verschickt. Ein Ordner voller `crash-<datum>.log` von drei PCs ist hinterher
nicht mehr auseinanderzuhalten.

Am Kasten hängen vier Handlungen, und drei davon stehen unter
[*Export & Anzeige → Protokolle*](interface/export-and-display.md#protokolle)
auch an jeder Datei: Ansehen, Herunterladen
und — dort nur bei einem Absturzbericht — der GitHub-Knopf. Das ✓ gibt es nur
am Kasten.

| Zeichen | Was es tut |
|---|---|
| 👁 | den Bericht im Browser ansehen |
| ⤓ | ihn als Datei herunterladen |
| GitHub | GitHub mit ausgefülltem Fehlerbericht öffnen |
| ✓ | den Hinweis wegnehmen (die Datei bleibt) |

**Geöffnet, nicht abgeschickt.** Was in dem Bericht steht, ist eine feste Liste:
Version, Windows-Fassung, Anzeigesprache, Prozessor, Grafikkarte, welche
GPU-Quellen geantwortet haben, ob erhöht gelaufen wurde, und die Aufzeichnung
selbst. Die Konfiguration wird bewusst nicht gelesen, denn dort stehen das
Broker-Passwort und drei Tokens.

Vor dem Ablegen wird der Bericht gewaschen — auch die **Datei**, nicht nur der
Link. Ersetzt durch `<removed>` werden: der eigene Benutzerpfad, Zugangsdaten
in URLs (`tcp://name:passwort@broker`), und jeder Schlüssel, dessen Name auf
`password`, `passwd`, `token`, `secret`, `apikey` oder `api_key` endet — also
auch `mqtt_password` und `influx_token`, in der Form `name=wert` wie in JSON.
Dazu ein `Bearer <wert>` in einem Kopfzeilenfeld.

**Lies den Bericht trotzdem, bevor du ihn anhängst.** Die Wäsche ist eine
Rückfallsicherung für den Fall, dass irgendwann eine Logzeile dazukommt, die an
all das nicht denkt — keine Garantie. Abgeschickt wird nichts, bevor du es auf
der GitHub-Seite selbst tust; der Knopf lässt sich unter
[*Export & Anzeige → Anwendung*](interface/export-and-display.md#anwendung) auch
ganz abschalten.

Angeboten wird das auch für eine Sitzung, die einfach verschwunden ist, nicht
nur für eine Panik mit Stapelspur. Ein Programm, das wortlos weg ist, ist genau
der Fehler, für den das hier gebaut wurde, und der Bericht trägt Build,
Maschine, antwortende Quellen und die letzten 200 Logzeilen. Ob jemand die
Aufgabe absichtlich beendet hat, kann das Programm nicht wissen — der Kasten
fragt es, und wer ihn liest, weiß es.

Aufgezeichnet wird in jedem Fall. Ob ein Absturz auffällt, ist keine
Einstellung.

## Der Akku

Die Akkugruppe ist die einzige, die auf den meisten Rechnern leer bleibt, und
das ist Absicht: ein Desktop erzeugt hier **keine einzige Entity**. Eine Anzeige,
die dauerhaft „0 %" behauptet, wäre die schlechtere Antwort als gar keine. Auf
der Anzeigeseite fehlt dort auch der Kasten, und auf der Seite **Messwerte**
fehlen die Akku-Zeilen — eine fehlende Grafikkarte ist eine Meldung wert, ein
fehlender Akku in einem Tower nicht. Ein Akku, der da ist und nicht antwortet,
wird dagegen gemeldet. Die Auswahl selbst bleibt dabei unangetastet: die
Konfiguration einer Maschine ohne Akku behält die Akku-Messwerte, sodass
dieselbe Datei auf einem Laptop vollständig ankommt.

Zwei Quellen speisen sie, und sie beantworten verschiedene Fragen. Die
Energieschnittstelle von Windows sagt, wie es dem Akku **gerade** geht — wie
voll, am Netz oder nicht, ladend oder entladend, wie lange er noch reicht. Das
Akkugerät selbst, über SetupAPI und die Akku-IOCTLs, sagt, **was** der Akku ist:
wie groß er neu war, wie viele Ladezyklen er hinter sich hat, woraus er besteht.
Nur der zweite Weg kann etwas über Verschleiß sagen, und nur der erste ist
billig genug, ihn alle paar Sekunden zu gehen — die Gerätewerte werden deshalb
alle fünf Minuten neu geholt.

Keiner der beiden Wege braucht Administratorrechte, WMI oder einen fremden
Treiber.

Der **Zustand** (`battery_health`) ist die Ladekapazität von heute geteilt durch
die Designkapazität — herum gesagt, dass ein frischer Akku bei 100 steht und
nach unten wandert; das zeichnet sich in Home Assistant besser als der
Kehrwert. Design- und Ladekapazität stehen daneben, damit die Zahl nachrechenbar
ist statt geglaubt.

Die **Leistung** (`battery_power`) ist vorzeichenbehaftet: positiv, während der
Akku Ladung aufnimmt, negativ, während er sie abgibt. Eine Reihe zeigt damit das
ganze Bild statt zwei, von denen nie beide gleichzeitig interessant sind.

Weggelassen wird, was der Akku nicht hergibt, und das ist mehr, als man denkt.
Viele Controller zählen **keine Ladezyklen** und melden dauerhaft 0 — daraus
entsteht keine Entity, denn „0 Zyklen" liest sich wie ein fabrikneuer Akku.
Die Restlaufzeit gibt es nur beim Entladen. Meldet ein Controller seine
Kapazitäten in eigenen Einheiten statt in Milliwattstunden, entfallen alle
Wh-Werte; Ladestand, Zustand und Zyklen bleiben, weil sie davon nicht abhängen.

Serien- und Herstellernummer des Akkus wären über denselben Weg zu haben und
werden bewusst **nicht** gemeldet: identifizierend, ohne irgendetwas über den
Zustand der Maschine zu sagen.

Ein Gerät mit zwei Akkus meldet trotzdem einen: Windows fasst die Livewerte
ohnehin zusammen, die Kapazitäten werden addiert, und als Zyklenzahl gilt die
höhere der beiden — die müdere Zelle ist die Antwort, auf die es ankommt.

---

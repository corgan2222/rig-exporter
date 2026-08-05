![rig-exporter](docs/images/github-banner-1280x300.png)

# rig-exporter

[![CI](https://github.com/corgan2222/rig-exporter/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/corgan2222/rig-exporter/actions/workflows/ci.yml)
[![Release](https://github.com/corgan2222/rig-exporter/actions/workflows/release.yml/badge.svg)](https://github.com/corgan2222/rig-exporter/actions/workflows/release.yml)
[![Neuestes Release](https://img.shields.io/github/v/release/corgan2222/rig-exporter?label=release&color=blue)](https://github.com/corgan2222/rig-exporter/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/corgan2222/rig-exporter/total?label=downloads&color=blue)](https://github.com/corgan2222/rig-exporter/releases)

Telemetrie eines Gaming-PCs für Home Assistant, Prometheus und InfluxDB.

Liest die FPS aus dem RivaTuner Statistics Server, erkennt das laufende Spiel
und meldet dazu Grafikkarte, Prozessor, Laufwerke, Netzwerk, Latenz und, wo
einer da ist, den Akku — per
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

Quer über alle Gruppen liegt die Wahl zwischen zwei **Messwertsätzen**. Die
Gruppen sagen, welche Hardware gelesen wird; der Satz sagt, wie ausführlich:

* **Standard** — 72 Messwerte: was man sich ansieht, wenn man wissen will, wie
  es dem Rechner geht. Temperatur, Auslastung, freier Platz, Durchsatz, FPS.
* **Erweitert** (Voreinstellung) — die übrigen 42 dazu: Taktraten, Speicher­riegel,
  Last je Thread, Anzeigemodus, Zustand von RTSS, Akkuverschleiß. Nützlich beim
  Suchen eines Problems, im Alltag selten.

Wie viele Entities daraus auf einem bestimmten Rechner werden, sagt die
Einstellungsseite oben an — sie zählt, was dieser PC tatsächlich hergibt.
Welcher Messwert in welchem Satz steckt, listet sie ausklappbar auf — erzeugt
aus dem Katalog, nicht abgetippt. Die Auswahl wirkt überall gleich: was der Standard­satz
nicht enthält, entsteht gar nicht erst und fehlt deshalb in MQTT, JSON,
Prometheus, InfluxDB **und** auf der eigenen Anzeigeseite. Ein Wert, den das
Dashboard zeigt und Home Assistant nie bekommt, wäre die schlechtere Einstellung.

Beim Wechsel auf den Standardsatz werden die abgewählten Entities in Home
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
| **FPS & System** (immer an) | FPS, Frametime, laufendes Spiel, Auflösung, Bildwiederholrate, CPU-Last, RAM-Last, Windows-Version, Anzahl Prozesse, Laufzeit, Leerlaufzeit | RTSS + Windows |
| **Grafikkarte** | Name, Hersteller, Treiberversion, dedizierter und gemeinsam nutzbarer Speicher, Temperatur, Hotspot, Kern- und Speichertakt, Auslastung und ihre Aufteilung auf 3D, Videodekodierung, Videokodierung und Kopier-Engine, belegter Grafikspeicher, VRAM, Lüfter (% und U/min), Leistung, Leistungsgrenze und deren Ausschöpfung, Spannung — pro Karte | Windows DXGI, Plug and Play und die WDDM-Leistungsindikatoren, MSI Afterburner und NVML |
| **Prozessor** | Modell, Hersteller, Kerne, Threads, Basis-, wirksamer und höchster beobachteter Takt, Temperatur, Leistung, Load über 1/5/15 Minuten, optional Last je Thread | Windows, Temperatur über Afterburner oder PawnIO, Leistung nur über PawnIO (AMD, eleviert) |
| **Arbeitsspeicher** | belegt und frei in MB, frei in %, gesamt, Takt, maximaler Takt, Typ, bestückte und vorhandene Steckplätze, ein Eintrag je Modul | Windows + SMBIOS der Firmware |
| **Laufwerke** | Typ (NVMe/SSD/HDD), Label, Dateisystem, Hersteller, Kapazität, belegt, frei, Belegung und freier Anteil in %, Lesen, Schreiben, Auslastung — pro Volume, dazu fünf Summenwerte über alle | Windows |
| **Netzwerk** | Adapter, Link-Speed, Download- und Upload-Rate, empfangene und gesendete Gesamtmenge, Fehler, verworfene Pakete, WLAN-Signal, Ping und Paketverlust | Windows + ICMP |
| **Akku** | Ladestand, Netzbetrieb, Laden, Restenergie, Lade- bzw. Entladeleistung, Restlaufzeit; im erweiterten Satz Zustand, Ladezyklen, Design- und Ladekapazität, Chemie, Spannung | Windows Power-API + Akkugerät |
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

### Woher die Grafikwerte kommen

Windows kennt jede Grafikkarte selbst: DXGI liefert Modell, PCI-Hersteller,
dedizierten Grafikspeicher und die Obergrenze des gemeinsam nutzbaren
Systemspeichers; Plug and Play ergänzt die installierte Treiberversion.
Temperatur, Takt und Leistung gehören dagegen nicht zu diesen Schnittstellen.
Deshalb greifen vier Quellen ineinander:

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

Die Leistungsindikatoren geben die Auslastung nach **Engine** aufgeschlüsselt:
3D, Videodekodierung, Videokodierung und Kopier-Engine, jede als Summe über alle
Prozesse. Zusammengezählt werden sie nicht — drei Engines zu je 60 % ergäben
180 %, und mehr als voll beschäftigt kann eine Karte nicht sein. Der
Gesamtwert ist deshalb die **belegteste** Engine, wie im Task-Manager.

`gpu_load` wird daraus nur gefüllt, wenn weder Afterburner noch NVML ihn
geliefert haben. Auf einem Rechner mit NVIDIA-Karte ändert sich also nichts; auf
einem Laptop mit reiner Intel-Grafik gibt es damit zum ersten Mal überhaupt eine
GPU-Auslastung. Der belegte Grafikspeicher bekommt dagegen einen **eigenen**
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
kennt, etwa Hotspot und Spannung. Auf Intel und AMD bleibt ohne Live-Quelle das
DXGI-Inventar sichtbar; nicht messbare Werte werden weggelassen statt als null
behauptet. NVML meldet auch die Lüfterdrehzahl (`nvmlDeviceGetFanSpeedRPM`,
gemeldet wird der schnellste Lüfter der Karte) und wächst mit jeder
Treibergeneration um neue Einsprungpunkte, und `LazyProc.Call` löst das Symbol
über `mustFind` auf — das **panict**, wenn es fehlt. In einem Binary mit
`-H windowsgui` stirbt damit das Tray wortlos. Deshalb wird jeder Einsprungpunkt
einmal aufgelöst und vor dem ersten Aufruf geprüft; ein alter Treiber verliert
einen Wert, nicht das Programm.

Ohne Afterburner und NVML entfallen also nur Temperatur, Takt, Lüfter und
Leistung — Inventar, Auslastung und Speicherbelegung bleiben. Ohne
Kernel-Treiber sind Gehäuselüfter, Netzteil-Telemetrie und Spannungen
grundsätzlich nicht erreichbar.

Die drei Quellen zählen unabhängig voneinander durch. DXGI legt die Instanzen
fest, Afterburner und NVML werden über den Kartennamen darauf abgebildet — der
ist allerdings nicht eindeutig, zwei gleiche Karten heißen gleich. Zugeordnet
wird darum in Indexreihenfolge und jede Instanz höchstens einmal. Zusätzlich
begrenzt die Plug-and-Play-Geräteliste, wie oft dieselbe PCI-Kennung vorkommen
darf: Ein Citrix-Sitzungsadapter kann sonst eine echte Karte in DXGI spiegeln,
während zwei wirklich eingebaute gleiche Karten erhalten bleiben. Auf einem
Hybrid-Notebook vertauscht eine abweichende Aufzählungsreihenfolge dadurch
nicht mehr Intel- und NVIDIA-Werte.

### Alle Laufwerke zusammen

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
gehören deshalb zum **Standardsatz**.

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

### Der aktive Netzwerkadapter

Standardmäßig wird nur der Adapter gemeldet, über den die Default-Route läuft.
Ein Rechner mit Hyper-V, WSL, VPN und Capture-Treiber hat sonst schnell ein
Dutzend Interfaces, und das eine, das zählt, geht darin unter. Umschaltbar über
**Alle Adapter statt nur dem aktiven**.

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

Die beiden Raten hießen bis 1.5.2 „Empfangen" und „Gesendet" — was sich auf
einer Geräteseite wie eine Summe liest, obwohl Mbit/s eine Geschwindigkeit ist.
Nur der Anzeigename hat sich geändert; `net_rx` und `net_tx` heißen weiter so,
weil Dashboards darauf zeigen.

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

### Der Akku

Die Akkugruppe ist die einzige, die auf den meisten Rechnern leer bleibt, und
das ist Absicht: ein Desktop erzeugt hier **keine einzige Entity**. Eine Anzeige,
die dauerhaft „0 %" behauptet, wäre die schlechtere Antwort als gar keine. Auf
der Anzeigeseite fehlt dort auch der Kasten — eine fehlende Grafikkarte ist eine
Meldung wert, ein fehlender Akku in einem Tower nicht. Ein Akku, der da ist und
nicht antwortet, wird dagegen gemeldet.

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

## Voraussetzungen

* Windows 10/11 (64 Bit)
* [RivaTuner Statistics Server](https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/) für die FPS — auch in MSI Afterburner enthalten
* [MSI Afterburner](https://www.msi.com/Landing/afterburner/graphics-cards) für GPU- und CPU-Temperaturen
* Zum Bauen: Go 1.26 oder neuer (`go.mod` verlangt 1.26.5, die Abhängigkeiten
  für sich genommen schon 1.25). Kein CGO, kein C-Compiler.

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
Microsofts Treiber-Sperrliste steht. Damit sind Prozessortemperatur und
-leistung auch ohne Afterburner lesbar, und die Leistung überhaupt nur so.

**Bisher nur auf AMD.** Genutzt wird das Modul `AMDFamily17.bin`, das die
Familien 17h bis 1Ah abdeckt; auf allem anderen meldet die Quelle „only AMD
processors are supported so far" und liefert nichts. Auf einem Intel-Rechner
lohnt die Installation also derzeit nicht — dort ist Afterburner der Weg zur
CPU-Temperatur, und eine Package-Leistung gibt es gar nicht.

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

### Wie die Bezeichner aufgebaut sind

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
Hardware, welcher Messwert. Vorher stand der Rechnername am Ende, wo er beim
Auseinanderhalten zweier PCs nichts half.

Nicht überall ist das richtig. Ein Prozessorkern wird durch das Wort `cpu_core`
gezählt, `cpu_core_5` liest sich also bereits korrekt — `cpu_5_core` wäre
Unsinn. Dasselbe gilt für Speichermodule. Umgestellt wurden die drei
Dimensionen, bei denen das Gerät vor der Messgröße gehört: Grafikkarten,
Laufwerke, Netzwerkadapter.

**Bestehende Installationen:** die Entitäten heißen in Home Assistant anders und
müssen in Dashboards und Automatisierungen einmal umgehängt werden. Jede frühere
Schreibweise räumt das Programm beim nächsten Verbinden selbst ab.

Das ist nötig, weil eine Discovery-Nachricht **retained** ist: sie liegt auf dem
Broker und überlebt sowohl dieses Programm als auch das Löschen der Entität von
Hand — die kommt beim nächsten Neustart von Home Assistant einfach wieder.
Deshalb wird jeder alte Name ausdrücklich mit einer leeren Nachricht
zurückgenommen. In ein Topic zu schreiben, das es nie gab, tut nichts, also
braucht das kein Migrationsflag und kein Gedächtnis.

### Wo Home Assistant die Werte einsortiert

53 Messwerte stehen im Hauptbereich, 34 unter **Diagnose**, 7 werden gar nicht
als Entität veröffentlicht. Die Regel dahinter:

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

### Wer welchen Wert geliefert hat

Die Anzeigeseite hat ein Panel **Datenquellen**, und `-probe` denselben
Abschnitt: welche Quelle, wie viele Werte, und welche. Windows stellt überall
die große Mehrheit, DXGI, Afterburner und NVML teilen sich die Grafikwerte,
RivaTuner liefert die Bilder pro Sekunde, PawnIO die zwei Werte, die
Kernelrechte brauchen, und rig-exporter meldet seine eigene Version.

Die Summe liegt über der Zahl der Werte, weil Afterburner und NVML sich
überschneiden und der Zähler zeigt, wer geliefert *hat*, nicht wer gewonnen hat.

Das entsteht nicht aus einer Tabelle, sondern jede Messung wird beim Hinzufügen
gestempelt. Eine Tabelle beschriebe den gedachten Aufbau; so beschrieben wird
der Rechner vor dem Nutzer, einschließlich des Falls, dass ein Programm läuft
und trotzdem nichts beiträgt. Quellen mit mehreren Lieferanten korrigieren den
Stempel selbst — deshalb trennt die Grafikgruppe zwischen DXGI, Afterburner und
NVML, und die CPU-Temperatur erscheint als Afterburner-Wert, obwohl der Rest
der Prozessorquelle aus Windows kommt.

Die Frage, die das Panel beantwortet, ist: **was verliere ich, wenn ich dieses
Programm schließe.**

Die Herkunft erreicht **keinen** Export. Sie steht nicht in JSON, Prometheus
oder InfluxDB, denn sonst könnte ein Dashboard davon abhängen, welche
Hilfsprogramme auf einer Maschine zufällig laufen — das Gegenteil der Zusage,
dass derselbe Messwert aus jeder Quelle gleich aussieht.

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

`-Check` prüft Formatierung, führt `go vet`, `staticcheck` und die Tests aus und
baut danach. Ohne Flag wird nur gebaut. Ergebnis ist ein einzelnes
`rig-exporter.exe` (~11 MB) ohne weitere Dateien.

Die Icons werden mit `-Icon` neu erzeugt und liegen sonst fertig im Repository.
Eigener Schalter, weil `go run` dafür ein frisches, unsigniertes Binary in den
Build-Cache linkt und von dort startet — genau das Muster, das Microsoft
Defender heuristisch als `Trojan:Win32/Sabsik` meldet. Ein Warnhinweis bei jedem
Prüflauf ist den Schreck nicht wert.

Die beiden Abzeichen oben zeigen, ob das noch funktioniert. **CI** ist der
Prüflauf auf `main` — derselbe `build.ps1 -Check`, nur auf einem
GitHub-Windows-Läufer. **Release** ist der Lauf, der die veröffentlichten
Binaries baut und signiert; grün heißt, das zuletzt veröffentlichte Release ist
tatsächlich durchgebaut worden. Beide zeigen immer den **jüngsten** Lauf, nicht
einen bestimmten Stand — für ein einzelnes Release steht die Wahrheit auf seiner
eigenen Seite.

Das Skript prägt dabei eine Build-Kennung ein, die hinter der Version steht:

```
rig-exporter 1.5.1+<commits>.<hash>
```

Also Commit-Anzahl und Kurz-Hash, bei uncommitteten Änderungen zusätzlich
`.dirty`. Abgeleitet statt gepflegt, damit sie nicht von dem Code abweichen
kann, den sie beschreibt. Eine Versionsnummer allein beantwortet nie „ist das
das Binary mit der Korrektur" — sie bewegt sich zwischen Commits nicht. Ein
schlichtes `go build` lässt die Kennung leer, was ehrlich ist: dieses Binary kam
nicht aus dem Skript.

`tools/genicon` packt aus **einer** Quelle drei Dinge:
`docs/images/rig-exporter-entity-512.png` wird zu `icon.ico` für den
Infobereich, zu `rsrc_windows_amd64.syso` — der Windows-Ressourcendatei, die der
ausführbaren Datei ihr Symbol in Explorer, Taskleiste und Alt-Tab gibt — und zu
`icon.png`, das die Weboberfläche unter `/icon.png` ausliefert. Drei Bilder
desselben Programms, die sich widersprechen, wären schlimmer als keins.

Alle drei sind eingecheckt, damit ein blankes `go build` reicht. Die
Ressourcendatei wird von Hand geschrieben statt mit `rsrc` oder
`goversioninfo`, damit man kein Werkzeug installieren muss.

## Erster Start

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

## Oberfläche

Drei Seiten, erreichbar über die Kopfzeile:

* **Anzeige** — Live-Werte, Zustand der Exportziele, ein Panel je Sensorgruppe
  und die Adressen der aktiven Endpunkte. Aktualisiert sich im
  Auslese-Intervall.

  Unter den Kacheln sagen vier Chips, was gerade eingestellt ist: welcher
  Messwertsatz, ob mit Nachkommastellen, wie viele Entities entstehen, und in
  welchem Takt gesendet wird — dabei der Takt, der **gerade** gilt, mit dem
  Zusatz „im Spiel" oder „Leerlauf". Eine Zahl ohne diesen Zusatz wäre wertlos,
  weil es zwei davon gibt. Jeder Chip führt auf die Einstellung, die ihn
  bestimmt; einen Wert zu lesen und ihn zu ändern soll nicht zwei Suchen sein.
  Kacheln für Werte, die der gewählte Satz gar nicht misst, werden ausgeblendet
  statt leer angezeigt.

  Fehlt auf einem Rechner die GPU beziehungsweise werden dort bewusst keine
  Spieldaten genutzt, lässt sich der RTSS-Hinweis mit **„Keine GPU vorhanden —
  Spieldaten ausblenden“** dauerhaft wegräumen. Die Einstellung wird als
  `no_gpu` in `config.json` gespeichert und blendet zusätzlich die Kacheln FPS,
  Frametime und Spiel sowie den RTSS-Statuschip aus. Unter *Export & Anzeige →
  Anwendung* lässt sie sich wieder abschalten. Messung und Exporte ändern sich
  dadurch nicht.

  Beim ersten Besuch steht darunter ein Hinweis auf die wachsende
  Home-Assistant-Datenbank samt Verweis auf den fertigen `recorder:`-Abschnitt.
  „Gelesen, nicht wieder anzeigen" räumt ihn dauerhaft weg — in der
  Konfiguration gemerkt, anders als die Sortierung der Hardware-Panels.
* **Datengewinnung** — welcher Messwertsatz, welche Sensorgruppen gelesen
  werden und wie oft.
* **Export & Anzeige** — wohin die Werte gehen (MQTT, Home Assistant,
  Speicherung, Datenserver, InfluxDB) und wie sich die Anwendung selbst
  verhält. Unter den aktiven MQTT- und InfluxDB-Push-Zielen folgt eine
  Statuszeile der Verbindung: beim Aufbau gelb, im Betrieb grün und bei einem
  Fehler rot mit der letzten Fehlermeldung und einem Knopf zum Log. Die Seite
  aktualisiert beide Zustände alle drei Sekunden, ohne neu geladen zu werden.

Der Abschnitt **Speicherung** ist der einzige ohne Speichern-Button: er ändert
hier nichts, sondern erklärt, warum die Datenbank von Home Assistant schnell
wächst, und gibt den passenden `recorder:`-Block für genau diesen Rechner aus.

Jeder Abschnitt hat einen eigenen Speichern-Button, der erst grün wird, wenn in
genau diesem Abschnitt etwas geändert wurde. Gespeichert wird auch nur dieser
eine Abschnitt: ein Formular trägt keinen Beleg über Kästchen, die es gar nicht
enthält, und eine Teilübernahme würde sonst alles auf der anderen Seite
abschalten.

In den Hardware-Panels lässt sich zwischen zwei Sortierungen umschalten.
Voreingestellt ist **nach Gerät**: alles zu GPU 0 zusammen, dann alles zu GPU 1,
jede Platte für sich, jeder Adapter für sich. **Nach Messwert** listet
stattdessen gleichartige Werte untereinander und ist der Sonderfall — beim
Vergleichen zweier Karten nützlich, sonst nicht. Die Wahl bleibt im Browser
gespeichert.

Rechts in der Kopfzeile steht der Sprachumschalter. Er wirkt auf Oberfläche,
Tray-Menü, Dialoge und die angezeigten Entity-Namen in Home Assistant. Was er
ausdrücklich **nicht** anfasst, sind die Kennungen: `default_entity_id`,
`object_id`, `unique_id` und die Wertvorlage bleiben gleich, weil Dashboards und
Automatisierungen daran hängen.
Eine Entity-ID wie `sensor.re_corganpc2_fps` heißt in beiden Sprachen gleich,
nur der angezeigte Name wechselt. Dashboards und Automationen überleben einen
Sprachwechsel also unbeschadet.

Nicht übersetzt wird, was Maschinen lesen: Prometheus-Hilfetexte, Logzeilen und
Fehlermeldungen bleiben englisch.

Unten auf jeder Seite öffnen drei Schaltflächen die Konfiguration, das Log und
den Ordner darum. Der Umweg über den Server ist nötig, weil ein Browser einem
`file://`-Link von einer `http`-Seite aus nicht folgt.

### Oberfläche im Netzwerk

Voreingestellt lauscht die Oberfläche nur auf `127.0.0.1` — erreichbar also von
diesem Rechner und von sonst niemandem. **Diese Seite im Netzwerk erreichbar
machen** unter *Anwendung* bindet stattdessen an `0.0.0.0`, womit sie unter der
LAN-Adresse dieses PCs offensteht. Wirkt nach einem Neustart, wie die
Portänderung darüber.

> **Was das bedeutet:** auf dieser Seite stehen alle Einstellungen, das
> MQTT-Passwort und das InfluxDB-Token eingeschlossen, und es gibt **keine
> Anmeldung**. Wer die Adresse kennt, kann alles lesen und ändern. Nur in einem
> Netz einschalten, dem man vertraut — und niemals ins Internet weiterleiten.

Zwei Dinge folgen automatisch mit: der oberste Tray-Eintrag und der
„Visit"-Link auf der Home-Assistant-Geräteseite zeigen dann auf die LAN-Adresse
statt auf `127.0.0.1`. Der Link funktioniert damit auch vom Handy aus. Die
Adresse wird über die Default-Route ermittelt und nicht aus dem Rechnernamen
gebaut, weil eine Adresse auch dort funktioniert, wo die Namensauflösung im
lokalen Netz es nicht tut.

Der Datenserver darunter ist davon unabhängig: der lauscht seit jeher auf
`0.0.0.0`, kennt aber ein Token.

## Auslesen und Senden

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
ein Block **Langzeitspeicherung**, der die dritte Stellschraube erklärt und
gleich den passenden `recorder:`-Abschnitt für diesen PC ausgibt. Der Abschnitt
wird aus den Entities gebaut, die gerade wirklich existieren: zwei Grafikkarten
ergeben zwei Temperaturzeilen, eine abgeschaltete Sensorgruppe keine. Er gehört
in die `configuration.yaml` von Home Assistant und braucht dort einen Neustart.

Wichtig dabei: ein Ausschluss nimmt einer Entity **auch** die Langzeitstatistik,
nicht nur den Verlauf. Beides zusammen oder gar nicht.

**Berechne Nachkommastellen** ist der zweite Hebel gegen eine volllaufende
Datenbank. Ausgeschaltet werden alle Zahlen ganzzahlig gerechnet und gesendet —
in jedem Format, nicht nur in MQTT, damit Prometheus und Home Assistant sich
nicht über denselben Messwert uneinig sind. Ein Wert muss sich dann um eine
ganze Einheit bewegen, bevor er überhaupt als geändert zählt, und Home
Assistant schreibt nur Änderungen in seine Datenbank. Die Discovery meldet die
Genauigkeit passend mit, sonst stünde in der Oberfläche überall `x.0`.

---

## Exportziele

### 1. MQTT (Push)

Standardmäßig an. Ein Gerät in Home Assistant, dessen Entities sagen, woher sie
kommen:

```
sensor.re_corganpc2_fps            sensor.re_corganpc2_gpu0_temperature
sensor.re_corganpc2_cpu            sensor.re_corganpc2_diskc_used_percent
sensor.re_corganpc2_game           sensor.re_corganpc2_net_ethernet_2_rx
sensor.re_corganpc2_ping_rtt       binary_sensor.re_corganpc2_rtss
```

Aufbau siehe [Wie die Bezeichner aufgebaut sind](#wie-die-bezeichner-aufgebaut-sind).

Die Kennung wird über `default_entity_id` angefordert. `object_id` tat das
früher und wurde in Home Assistant 2026 aus der MQTT-Komponente entfernt —
gesendet werden beide, damit ältere Fassungen weiter bedient sind. Ohne eines
von beiden baut Home Assistant die Kennung aus Gerätename und Entitätsname
zusammen, und aus `re_corganpc2_diskc_busy` wird `corganpc2_busy_c`.

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

**Der „Visit"-Link** auf der Geräteseite in Home Assistant zeigt auf die
Oberfläche dieses Exporters — und zwar auf den Port, auf dem sie *tatsächlich*
lauscht. Ist der eingestellte Port belegt, weicht der Webserver auf einen
zufälligen aus; die Discovery-Nachricht wird dann mit der richtigen Adresse neu
geschrieben, sobald sie feststeht. Das ist nötig, weil Discovery retained ist:
eine einmal mit dem falschen Port veröffentlichte Nachricht bliebe sonst falsch,
bis sie jemand überschreibt.

Welche Adresse dort steht, hängt davon ab, worauf der Server lauscht. In der
Voreinstellung ist das nur Loopback, also `http://127.0.0.1:<port>` — der Link
funktioniert dann, wenn Home Assistant im Browser **auf diesem PC** geöffnet
ist, vom Handy aus nicht. Ist **Diese Seite im Netzwerk erreichbar machen**
gesetzt, steht dort die LAN-Adresse dieses Rechners und der Link funktioniert
von überall. Siehe [Oberfläche im Netzwerk](#oberfläche-im-netzwerk).

#### Updates

Geprüft wird direkt beim Start und danach alle **sechs Stunden**. Abschaltbar
unter **Export & Anzeige → Anwendung → Auf neue Versionen prüfen**; ab Werk an.
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
        value_template: "{{ value_json.gpu0_temperature }}"
        unit_of_measurement: "°C"
        device_class: temperature
        state_class: measurement
      - name: SSD C CorganPC2
        unique_id: disk_c_corganpc2_rest
        value_template: "{{ value_json.diskc_used_percent }}"
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

Direkt unter dem Kasten steht, was das Ziel gerade tut: Ziel und Anzahl
gesendeter Messungen, solange es läuft — und andernfalls die letzte
Fehlermeldung im Wortlaut, mit einem Knopf, der das Log öffnet. Push ist das
Ziel, das aktiv hinausschreibt und damit scheitern kann, ohne dass ein Abruf
den Fehler sichtbar macht; das Log zu suchen soll dann keine eigene Übung sein.
MQTT zeigt seinen Verbindungszustand auf dieselbe Weise.

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
Statuszeile je aktivem Exportziel und den RTSS-Status. Weitere Aktionen: Senden
pausieren, Einstellungen öffnen, Log öffnen, Autostart mit Windows, Beenden.
Fehlt RTSS, kommt ein Eintrag zum Download dazu.

Ganz oben stehen Name, Version und die Adresse, unter der die Oberfläche
erreichbar ist — `rig-exporter 1.5.1+<commits>.<hash> — 127.0.0.1:8787`. Ein Klick
darauf öffnet sie. Die Adresse steht dort, weil sie nicht immer die
eingestellte ist: ist der Port belegt, weicht der Server auf einen zufälligen
aus, und dann ist das der einzige Ort, an dem die richtige Nummer steht.

## Diagnose

```bash
.\rig-exporter.exe -probe
```

Nimmt zwei Messungen im Abstand von vier Sekunden und schreibt alles heraus,
gruppiert nach Sensorgruppe, gefolgt von JSON, Prometheus-Exposition und Line
Protocol. Der schnellste Weg zu sehen, welche Quellen greifen und was bei Home
Assistant ankäme.

Auf einem Laptop ohne Afterburner oder NVIDIA-Treiber muss der Abschnitt
**Grafikkarte** im erweiterten Messwertsatz mindestens Name, Hersteller,
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
| FPS bleibt 0, Spiel `none` | RTSS hookt die Anwendung nicht. Im RTSS-Profil „Application detection level" prüfen. |
| Keine GPU-Gruppe | GPU-Gruppe in den Einstellungen aktiv? DXGI und die Windows-Geräteliste fanden keinen physischen Adapter. Ein nicht erreichbarer Afterburner betrifft nur die Live-Werte. |
| Keine CPU-Temperatur | Kommt über Afterburner, oder über PawnIO — das aber nur auf AMD und nur eleviert. |
| Keine CPU-Leistung | Gibt es ausschließlich über PawnIO: eingeschaltet, AMD, eleviert. |
| Keine Durchsatzwerte | Erst ab der zweiten Messung vorhanden, sie sind eine Differenz. |
| Entities fehlen in HA | MQTT-Integration aktiv? Discovery-Präfix identisch? Log prüfen. |

### Was der Exporter selbst kostet

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

### Top-Prozesse

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

#### Die Form: ein Sensor mit Tabelle statt fünf Entities

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

#### Nachkommastellen: hier immer, und bei der CPU zwei

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

#### Die aktuelle Liste, ohne Zusatzkarte

Eine Markdown-Karte reicht und braucht kein HACS. Die Nummerierung ist das, was
sie mit der Legende des Diagramms verbindet:

```yaml
type: markdown
entity_id:
  - sensor.re_corganpc2_top_cpu
content: |
  {% set apps = state_attr('sensor.re_corganpc2_top_cpu','apps') %}
  {% if apps %}{% for app in apps %}**{{ loop.index }}.** {{ app.name }} — **{{ app.value }} %**

  {% endfor %}{% else %}_noch keine Messung_{% endif %}
```

#### Säulendiagramm über die Zeit

Dafür braucht es **ApexCharts Card** aus HACS. Die vollständige Karte, für den
Speicher genauso mit `sensor.re_corganpc2_top_memory`:

```yaml
type: custom:apexcharts-card
header:
  show: true
  title: CPU-Anteil der fünf größten Programme
graph_span: 6h
stacked: true
all_series_config:
  type: column
  extend_to: false
  group_by:
    func: avg
    duration: 5min
series:
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank1, name: Platz 1}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank2, name: Platz 2}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank3, name: Platz 3}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank4, name: Platz 4}
  - {entity: sensor.re_corganpc2_top_cpu, attribute: rank5, name: Platz 5}
apex_config:
  legend:
    formatter: >-
      EVAL:function (s) { try { var i = String(s).split(' ').pop();
      var a = document.querySelector('home-assistant').hass
        .states['sensor.re_corganpc2_top_cpu'].attributes;
      var n = a['rank' + i + '_name']; return n ? n : s; }
      catch (e) { return s; } }
  tooltip:
    y:
      title:
        formatter: >-
          EVAL:function (s) { try { var i = String(s).split(' ').pop();
          var a = document.querySelector('home-assistant').hass
            .states['sensor.re_corganpc2_top_cpu'].attributes;
          var n = a['rank' + i + '_name']; return n ? n : s; }
          catch (e) { return s; } }
```

Vier Stellen daran sind nicht optional:

**`attribute:` mit einem flachen Rang.** Das ist der ganze Mechanismus — die
Karte liest `attributes['rank1']` aus **jedem** Eintrag der History. Eine Liste
von Objekten kann sie nicht zeichnen.

**`group_by`.** Bei 10 s Messintervall liegen in 6 Stunden 2160 Punkte je Serie,
mal fünf Serien über 10 000 Säulen. Ohne Bündelung malt die Karte Striche.
`avg` und nicht `max`, weil sich gestapelte Maxima addieren, die nie gleichzeitig
auftraten — der Stapel stünde dann über 100 %.

**Legende *und* Tooltip.** Die beiden laufen durch verschiedene Formatter, und
die Karte setzt selbst nur `tooltip.y.formatter` für den *Wert*. Der Titel davor
bleibt sonst „Platz 1". Dass der eigene `title.formatter` den Wert-Formatter
nicht verdrängt — Einheit und Rundung bleiben —, liegt daran, dass `apex_config`
per Deep Merge übernommen wird.

**Das `try`/`catch`.** Ohne es nimmt ein Fehler im Formatter die ganze Karte mit;
mit ihm steht im schlimmsten Fall wieder „Platz 1" da.

Der Platz wird aus dem Seriennamen gelesen (`"Platz 3"` → `3`), weil der
Tooltip-Formatter nur den Namen bekommt und keinen Index. Wer die Serien
umbenennt, muss die Funktion mit umbauen.

> **Nicht `data_generator` benutzen.** Das liegt nahe, weil `apps` so schön
> passt — die Option „completely bypasses the history retrieval" und sieht nur
> den *aktuellen* Zustand. Sie kann damit nie mehr als einen Punkt erzeugen,
> und die Karte lädt endlos, statt einen Fehler zu zeigen. `data_generator` ist
> für Attribute gedacht, die selbst schon eine Zeitreihe enthalten, etwa eine
> Wettervorhersage.

Zwei Dinge, die man beim Lesen wissen muss: die beiden Formatter greifen über
`document.querySelector('home-assistant')` in die Interna des Frontends und
können mit einem Home-Assistant-Update brechen. Und der angezeigte Name ist
immer der **aktuelle** — über einer Säule von vor vier Stunden steht, wer
*jetzt* auf Platz 2 ist, nicht wer es damals war. Wer das nicht will, lässt die
`apex_config` weg und stellt die Markdown-Karte von oben darunter.

Prüfen lässt sich das Ganze in der Browser-Konsole, ohne auf einen Tooltip zu
zielen — synthetische Mouse-Events lösen bei ApexCharts ohnehin keinen aus:

```js
// durch die Shadow-Roots nach <apexcharts-card> laufen, dann:
const chart = card[Object.keys(card).find(k => k.toLowerCase().includes('apexchart'))];
chart.w.config.tooltip.y.title.formatter("Platz 1");   // -> "firefox.exe"
typeof chart.w.config.tooltip.y.formatter === 'function'; // -> true, Einheit intakt
```

In den anderen Exportformaten stellt sich die Frage nicht: Prometheus bekommt
eine Serie je Zeile mit `app`- und `rank`-Label, InfluxDB ein Feld je Platz.

```
rig_top_cpu_percent{host="corganpc2",app="Cyberpunk2077.exe",rank="1"} 62.40
rig_top_cpu_percent{host="corganpc2",app="firefox.exe",rank="2"} 4.15
```

Wer die Werte langfristig als Diagramm braucht und HACS meiden will, ist mit
Prometheus und Grafana deutlich besser bedient als mit Home Assistant.

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

**GPU-Inventar** kommt aus DXGI 1.1. `DXGI_ADAPTER_DESC1` liefert Name,
PCI-Kennung, dedizierten und gemeinsam nutzbaren Speicher; Plug and Play ergänzt
die Treiberversion und filtert gespiegelte Sitzungsadapter. **GPU-Livewerte**
kommen aus Afterburners Shared Memory. Die Sensornamen sind pro Karte
durchnummeriert (`GPU1 temperature`), und der Kartenindex im Eintrag ist nicht
verlässlich — Afterburner setzt ihn auch bei „RAM usage" —, deshalb wird über
den Namen auf die DXGI-Instanz abgebildet.
NVML-Karten werden genauso über den Namen gepaart, nicht über den Index: zwei
Karten verschiedener Hersteller würden sonst vertauscht.

**Auflösung und Hz** kommen von `EnumDisplaySettingsW` des primären Monitors,
also aus dem Anzeigetreiber — unabhängig von der DPI-Skalierung des Prozesses.

**Windows-Version** ist zusammengesetzt, weil keine einzelne Quelle sie hat: die
Buildnummer aus `RtlGetVersion` (`GetVersionEx` lügt Programme ohne Manifest an
und nennt jedes Windows seit 8.1 „6.2"), Edition und Release aus der Registry.
Der Haken dabei ist, dass `ProductName` dort auch auf Windows 11 noch
„Windows 10 Pro" sagt — Microsoft hat den Wert nie nachgezogen. Wer ihm glaubt,
nennt der Hälfte seiner Nutzer das falsche Betriebssystem, also entscheidet die
Buildnummer: ab 22000 ist es Windows 11. Gelesen wird das einmal pro Start, denn
es kann sich unter einem laufenden Prozess nicht ändern.

**Anzahl der Prozesse** über `EnumProcesses`. Die Funktion meldet nicht, dass
der Puffer zu klein war — sie füllt, was hineinpasst, und sagt es nur dadurch,
dass sie genau so viele Bytes zurückgibt wie angeboten. Der Puffer wächst
deshalb, bis die Antwort kleiner ist als der Platz dafür.

**CPU-Last** ist die Differenz zweier `GetSystemTimes`-Abfragen, die Last je
Thread kommt aus `NtQuerySystemInformation`.

**CPU-Takt** ist der wirksame Takt, nicht der Basistakt. `CallNtPowerInformation`
wäre der naheliegende Weg, liefert auf jedem aktuellen AMD und den meisten Intel
aber unverändert den Nennwert — ein Prozessor, der gerade mit 4,2 GHz läuft,
meldet dort seinen Basistakt, und die Anzeige steht still. Der einzige bewegliche Wert ist der
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
internal/hardware/gpu            GPU-Gruppe: DXGI + WDDM-Zähler + Afterburner + NVML
internal/pdh                     Windows-Leistungsindikatoren, einzeln und mit Platzhalter
internal/hardware/cpu            CPU-Gruppe
internal/hardware/ram            Speichergruppe, inklusive SMBIOS-Parser
internal/hardware/disk           Laufwerksgruppe
internal/hardware/net            Netzwerkgruppe und Latenzmessung
internal/hardware/battery        Akkugruppe: Livezustand und Verschleiß
internal/hardware/pawnio         PawnIO: Erkennung, Module, AMD-Dekodierung
internal/config                  Konfiguration, Grenzwerte, Entity-Kennungen
internal/export                  gemeinsame Schnittstelle der Exportziele
internal/export/dataserver       HTTP: JSON, Prometheus, Influx
internal/export/influxpush       Schreiben an InfluxDB
internal/hamqtt                  MQTT und Home-Assistant-Discovery
internal/app                     Messschleife, Konfigurationswechsel
internal/webui                   Einstellungsseite auf 127.0.0.1
internal/tray                    Infobereich-Symbol und Menü
internal/autostart               der Run-Eintrag in der Registry
internal/applog                  Logziel und -format
internal/assets                  das eingebettete Symbol
internal/winapi                  die Win32-Aufrufe, die x/sys nicht abdeckt
tools/genicon                    packt docs/images in internal/assets
```

Ein neuer Messwert wird einmal in `internal/metrics` eingetragen und erscheint
danach in allen vier Formaten — inklusive Home-Assistant-Darstellung, denn
Einheit, Device-Class und Icon stehen in derselben Definition. Die einzige
Stelle, an der eine Einstellung in die Definition hineinreicht, ist die
Nachkommastelle: sie lässt sich global auf null ziehen, dann aber in allen vier
Formaten gleich, damit kein Format eine andere Zahl zeigt als das andere. Der Name steht
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

287 Testfunktionen in 37 Dateien. Abgedeckt sind: die drei Parser, die
Metrikdefinition und ihre vier Ausgabeformate samt festgeschriebenem Katalog,
die Konfiguration mit Migration und Grenzwerten, die Übersetzungen, die
Home-Assistant-Discovery, die Exportziele, der Collector, die Messschleife mit
ihren zwei Sendetakten, die Web-Handler samt blockweisem Speichern und dem
erzeugten Recorder-Vorschlag, das DXGI-Inventar samt Plug-and-Play-Abgleich, die
Zuordnung zweier Grafikkarten zwischen DXGI, Afterburner und NVML, der
Load-Mittelwert — und bei PawnIO die
Zen-Dekodierung, die Modulnamen-Prüfung, die Beschränkung des Downloads auf
GitHub-Release-Hosts und die Zusicherung, dass ein Download nichts ausführt.

Nicht abgedeckt sind die Win32-Aufrufe selbst und die Windows-Hälften der
Hardware-Quellen — die brauchen die Hardware, die sie beschreiben. Sie werden
mit `-probe` gegen den echten Rechner geprüft. Auch das Tray-Menü und das
Icon-Werkzeug sind nur manuell verifiziert.

`go vet` läuft im Build-Skript mit `-unsafeptr=false`: das Mappen fremder
Shared-Memory-Blöcke braucht eine `uintptr`-Konvertierung, die vet nicht
beurteilen kann. Strukturen, deren Größe exakt zur Windows-Definition passen
muss, sichern sich stattdessen selbst über eine Größenzusicherung ab, die zur
Übersetzungszeit bricht.

### staticcheck

Dazu läuft [staticcheck](https://staticcheck.dev) mit **allen** Prüfungen, die
sein Kommandozeilenwerkzeug kennt — 149 an der Zahl: 95 zur Korrektheit, 35
Vereinfachungen, 18 zum Stil und eine für unbenutzten Code. Auch die, die
staticcheck nicht von sich aus einschaltet: Paketkommentare, Doku-Kommentare,
die mit dem Namen beginnen, den sie beschreiben, einheitliche
Empfängernamen. Der Code erfüllt sie ohnehin, das Einschalten kostet also
nichts und hält die Konvention fest.

Was in der Dokumentation als Gruppe **QF** steht, fehlt nicht aus Nachlässigkeit:
diese Prüfungen gibt es im Kommandozeilenwerkzeug gar nicht. Sie treiben
automatische Umformungen in gopls und beschreiben Vorlieben, keine Fehler.

Die Auswahl steht in `staticcheck.conf`. Ist das Werkzeug nicht installiert,
überspringt der Prüflauf es mit einem Hinweis; eine **veraltete** Fassung wird
dagegen abgelehnt. Vor Go 1.18 gebaute Versionen verstehen keine Generics und
scheitern mit seitenweise Unsinn über die Standardbibliothek statt mit einer
brauchbaren Meldung.

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

---

## Mitmachen

Fehlerberichte und Pull Requests sind willkommen. Die Regeln stehen in
[CONTRIBUTING.md](CONTRIBUTING.md) — die kurze Fassung: Änderungen kommen
ausschließlich über einen Pull Request, `.\build.ps1 -Check` muss grün sein, und
der Messwert-Vertrag in `internal/metrics/testdata/catalogue.txt` wird nur
bewusst geändert.

Bei einem Fehlerbericht ist die Ausgabe von `-probe` mehr wert als eine
Beschreibung. **Vor dem Einfügen lesen:** darin stehen der Rechnername,
Laufwerksbezeichnungen und Netzwerkadressen.

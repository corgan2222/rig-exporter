# Voraussetzungen

**Die einzige echte Voraussetzung ist Windows 10 oder 11 (64 Bit).**

Alles andere ist freiwillig. Herunterladen, Doppelklick, fertig: keine
Installation, keine Laufzeitumgebung, keine Bibliothek, kein
Administratorkonto. In diesem Zustand misst das Programm bereits den größten
Teil seines Katalogs — Prozessor, Arbeitsspeicher, Laufwerke, Netzwerk, Akku
und das Inventar der Grafikkarten liest Windows selbst.

Zusatzprogramme schalten jeweils einen klar umrissenen Bereich frei. Was fehlt,
wird weggelassen, nicht als Null erfunden, und taucht von selbst auf, sobald die
Quelle da ist — ein Neustart ist dafür nie nötig.

## Was welches Programm zusätzlich liefert

| Quelle | Nötig? | Was erst dadurch entsteht |
|---|---|---|
| **Windows allein** | schon da | Prozessor (Modell, Kerne, Takt, Auslastung), Arbeitsspeicher, Laufwerke, Netzwerk, Akku, Windows-Version, Latenz, Top-Prozesse, Eigenverbrauch — dazu das GPU-Inventar über DXGI und die GPU-Auslastung über die WDDM-Leistungsindikatoren |
| **Grafiktreiber** — NVML bei NVIDIA, ADLX bei AMD | schon da | GPU-Temperatur, Kern- und Speichertakt, VRAM-Gesamtausbau, Lüfterdrehzahl, Leistung. Auf AMD zählt ADLX ohne RTSS zusätzlich die Bilder pro Sekunde, daraus die Frametime — aber nur im Vollbild und ohne Spielnamen. Kommt mit dem Treiber, nichts zu installieren |
| [**RivaTuner Statistics Server**](https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/) | nein | **Bilder pro Sekunde, Frametime und der Name des laufenden Spiels.** Den Namen gibt es nur so; die Rate zählt sonst nur der AMD-Treiber, und der nur im Vollbild |
| [**MSI Afterburner**](https://www.msi.com/Landing/afterburner/graphics-cards) | nein | **CPU-Temperatur** auf jeder Plattform; dazu die GPU-Live-Werte dort, wo der Treiber sie nicht hergibt (Intel, ältere Karten). Enthält RTSS bereits |
| [**PawnIO**](https://pawnio.eu/) | nein | **CPU-Leistung in Watt** — nur so zu bekommen — und die CPU-Temperatur ohne Afterburner. Derzeit nur AMD, und nur mit Administratorrechten |

Die Zeilen sind kumulativ gemeint, aber nicht voneinander abhängig: RTSS ohne
Afterburner ist genauso sinnvoll wie Afterburner ohne RTSS. Wer nur wissen will,
wie warm und wie voll der Rechner ist, braucht keines von beiden.

Zum **Bauen** aus dem Quelltext: Go 1.26 oder neuer (`go.mod` verlangt 1.26.5,
die Abhängigkeiten für sich genommen schon 1.25). Kein CGO, kein C-Compiler.

## Rechte und laufende Programme

Nichts am Programm braucht Administratorrechte — mit der einen Ausnahme PawnIO,
und die schaltet man ausdrücklich selbst ein.

Eine Falle gibt es dabei: Läuft RTSS oder Afterburner **eleviert** und dieses
Programm nicht, verweigert Windows den Zugriff auf deren Shared Memory. Dann
fehlen FPS beziehungsweise die Temperaturen, obwohl beide Programme sichtbar
laufen. Entweder beide eleviert oder beide nicht.

Fehlt RTSS, erscheint **beim ersten Start** ein Hinweis mit Downloadlink —
danach nicht mehr. Alle übrigen Gruppen laufen ohne RTSS weiter, und der Zustand
steht im Tray und auf der Anzeigeseite. Ein Rechner ohne RTSS ist für alles
andere ein völlig brauchbarer Rechner, und nichts wartet hier auf einen Dialog.

Wird RTSS geschlossen, verschwindet sein Shared Memory **nicht**: RTSSHooks
bleibt in jeder eingeklinkten Anwendung geladen, der Abschnitt überlebt den
Prozess, und RTSS überschreibt beim Beenden seine Signatur mit `0xDEAD` —
laut eigenem SDK „zur Freigabe markiert". Das wird als „läuft nicht" gemeldet,
nicht als Fehler. Startet RTSS später, verbindet sich das Programm von selbst:
die Zuordnung wird bei jedem Auslesen neu geöffnet, ein Neustart ist nie nötig.

## PawnIO

Download: **[pawnio.eu](https://pawnio.eu/)** — dieselbe Adresse, die auch die
Seite „Datengewinnung" neben dem Kästchen verlinkt. Das Angebot beim ersten
Start lädt dagegen direkt vom GitHub-Release von PawnIO.Setup.

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

Die Module kommen aus einem **festen** Release von PawnIO.Modules, nicht aus dem
jeweils neuesten. Sie werden einmal geladen und unter
`%APPDATA%\rig-exporter\modules` behalten. Eine neue Modulversion kommt damit mit
einer neuen rig-exporter-Version und nicht von selbst — was bei signiertem Code,
der in einem Kerneltreiber landet, die richtige Richtung ist.

Das Laden hält die Messung nicht auf. Es läuft neben der Messschleife, und
solange nichts geladen ist, liefert diese Quelle nichts — alle anderen Werte
kommen unverändert weiter. Scheitert es, etwa weil gerade kein Netz da ist,
wird es später erneut versucht: zuerst nach einer Minute, dann in wachsendem
Abstand bis zu einer Stunde. Ein Laptop, der ins WLAN zurückkehrt, braucht
dafür keinen Neustart des Programms.

**CPU-Temperatur gibt es sonst nur mit Afterburner.** Das ist keine Bequemlichkeit:
Ryzen liefert Tctl über den SMU, Intel über ein MSR, und beides liegt in Ring 0.
Kein Programm ohne Kerneltreiber kommt daran — deshalb bringt Afterburner einen
mit. Die treiberfreien Wege sind nachgemessen und alle tot: ACPI-Thermalzonen
(über PDH, SetupDi und WMI je null Instanzen), `Win32_TemperatureProbe` (braucht
eine SMBIOS-Struktur, die Consumer-Boards nicht schreiben) und
`CallNtPowerInformation` (hat kein Temperaturfeld).


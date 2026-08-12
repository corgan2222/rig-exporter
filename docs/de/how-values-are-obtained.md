# Wie die Werte zustande kommen

**FPS und Spiel** kommen aus `RTSSSharedMemoryV2`. Der Block wird bei jedem
Intervall neu gemappt, gelesen und freigegeben, ein RTSS-Neustart also ohne
Zutun aufgefangen. Die Rate ist `1000 × Frames / (Time1 − Time0)`, genau das,
was der Overlay anzeigt.

Von allen gehookten Prozessen gewinnt der Vordergrundprozess, wenn RTSS ihn
kennt — das ist, worauf man gerade schaut. Sonst der zuletzt gerenderte, damit
ein Spiel im Hintergrund weiterzählt. Einträge, deren letztes Bild älter als das
Idle-Timeout ist, fallen raus; das lässt ein beendetes Spiel auf `none`
zurückfallen statt beim letzten Wert einzufrieren.

Hat RTSS nichts, springt der **Grafiktreiber** ein, sofern er selbst Bilder
zählt: AMDs ADLX tut das. Verdrängen kann er RTSS nicht, und er soll es auch
nicht — RTSS kennt Spielnamen, Prozess-ID und die Zeit, die das letzte Bild
wirklich gebraucht hat, und zählt auch im Fenstermodus. Der Treiber zählt
**nur im Vollbild** und weiß nicht, was da zeichnet; das Spiel bleibt deshalb
`none`. Die Frametime wird aus der Rate abgeleitet, so wie es schon bei
RTSS-Fassungen ohne eigenen Frametime-Zähler geschieht. Woher der Wert kam,
steht als `fps_origin` in `/api/status` und in der `-probe`-Ausgabe — auf dem
Weg in einen Export erscheint es nicht, dort sieht jeder Messwert gleich aus,
egal wer ihn gezählt hat.

**Welches Spiel diese ausführbare Datei ist**, wird nur ermittelt, wenn
[die Option](interface/data-capture.md#das-spiel-ermitteln) an ist, und zwar
über drei Quellen in der Reihenfolge ihrer Kosten. Steam schreibt die gestartete
App nach `HKCU\Software\Valve\Steam\RunningAppID` und den Titel, den es dafür
führt, nach `…\Steam\Apps\<id>\Name`: zwei Registry-Lesevorgänge, ohne
Elevation, ohne Zugriff auf den Prozess des Spiels, und nichts davon verlässt
den Rechner. Schweigt Steam, wird der von RTSS gemeldete Pfad gegen die Kataloge
gehalten, die GOG (`HKLM\SOFTWARE\WOW6432Node\GOG.com\Games`) und Epic
(`%ProgramData%\Epic\EpicGamesLauncher\Data\Manifests\*.item`) auf der Platte
führen — der längste passende Ordner gewinnt, damit ein Spiel im Verzeichnis
eines anderen als es selbst gemeldet wird. Add-ons fallen dabei heraus: sie
nennen den Ordner ihres Hauptspiels, und „Cyberpunk 2077: Phantom Liberty" würde
sonst auf die AppID der Erweiterung führen und das falsche Bild zeigen.

Nur ein so gefundener Titel, der immer noch keine AppID hat, ist eine Anfrage an
die öffentliche Steam-Suche wert — dieselbe, die das Suchfeld der Store-Seite
benutzt, ohne Schlüssel und ohne Konto. Das ist das Einzige an diesem Programm,
das den Rechner verlässt: hinaus geht der Spielname, zurück kommt eine AppID,
einmal je Titel, und die Antwort bleibt im Speicher — auch wenn sie leer war.
Gewartet wird darauf ebenfalls nie; ein Titel, dessen AppID noch nicht da ist,
wird ohne sie veröffentlicht und bekommt sie bei einer späteren Messung. Ein
langsamer Store darf kein langsamer Exporter werden.

Zwei andere Wege zu Steam sind gemessen und verworfen worden. `steam_appid.txt`
liegt bei drei der installierten Spiele auf dem Entwicklungsrechner, weil es
eine Entwicklerdatei ist und nichts, was jedes Spiel mitbringt. Die
Umgebungsvariable `SteamAppId` aus dem Prozess des Spiels zu lesen braucht
`ReadProcessMemory` gegen einen möglicherweise elevierten Prozess — fragil, und
genau die Form, nach der ein Virenscanner sucht.

**Ob die Maschine virtuell ist**, steht in der Firmware-Kennung: Hersteller,
Produktname und BIOS-Hersteller, die Windows aus den SMBIOS-Tabellen unter
`HKLM\HARDWARE\DESCRIPTION\System\BIOS` ablegt. Ein Gast nennt sich dort selbst
— `QEMU` / `Standard PC (i440FX + PIIX, 1996)`, `VMware, Inc.`, `innotek GmbH`,
`Microsoft Corporation` / `Virtual Machine`.

Bewusst **nicht** über das Hypervisor-Bit des Prozessors, obwohl es näher läge:
Windows setzt das auch auf echter Hardware, sobald Hyper-V, WSL 2 oder die
Speicherintegrität aktiv ist — jedes davon setzt den Wirt selbst auf einen
Hypervisor. Ein Spiele-PC mit eingeschalteter VBS würde sich damit als virtuelle
Maschine melden, und eine falsche Ja-Antwort ist hier der teure Fehler: sie
schickt jemanden auf Fehlersuche bei dem einen Wert, der stimmt.

Die Nein-Antwort ist schwächer als die Ja-Antwort, und `virtualized` heißt
deshalb genau „keine bekannte Kennung gefunden". Ein Hypervisor lässt sich so
einstellen, dass er die Kennung des Wirt-Boards durchreicht; dann ist hier
nichts zu sehen. Der Name landet in `hypervisor` und fehlt auf echter Hardware
ganz, statt als leerer Text zu erscheinen.

**GPU-Inventar** kommt aus DXGI 1.1. `DXGI_ADAPTER_DESC1` liefert Name,
PCI-Kennung, dedizierten und gemeinsam nutzbaren Speicher; Plug and Play ergänzt
die Treiberversion und filtert gespiegelte Sitzungsadapter. **GPU-Livewerte**
kommen aus mehreren Quellen: Afterburner wird zuerst gelesen, und wo es
antwortet, gilt sein Wert — es ist die einzige Quelle, die auf jeder Karte
funktioniert. Die Lücken füllen die Herstellerbibliotheken, jede nur für die
Karten, die sie kennt: auf einer Radeon liefert ADLX Temperatur, Takt, Lüfter
und Leistung ohne jedes Zusatzprogramm, auf einer GeForce tut NVML dasselbe.

Aus Afterburners Shared Memory gelesen: die Sensornamen sind pro Karte
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

## Wer welchen Wert geliefert hat

Die Anzeigeseite hat ein Panel **Datenquellen**, und `-probe` denselben
Abschnitt: welche Quelle, wie viele Werte, und welche. Windows stellt überall
die große Mehrheit, DXGI, Afterburner, NVML und ADLX teilen sich die
Grafikwerte, RivaTuner liefert die Bilder pro Sekunde, PawnIO die zwei Werte,
die Kernelrechte brauchen, und rig-exporter meldet seine eigene Version.

Die Summe liegt über der Zahl der Werte, weil Afterburner und NVML sich
überschneiden und der Zähler zeigt, wer geliefert *hat*, nicht wer gewonnen hat.

Das entsteht nicht aus einer Tabelle, sondern jede Messung wird beim Hinzufügen
gestempelt. Eine Tabelle beschriebe den gedachten Aufbau; so beschrieben wird
der Rechner vor dem Nutzer, einschließlich des Falls, dass ein Programm läuft
und trotzdem nichts beiträgt. Quellen mit mehreren Lieferanten korrigieren den
Stempel selbst — deshalb trennt die Grafikgruppe zwischen DXGI, Afterburner,
NVML und ADLX, und die CPU-Temperatur erscheint als Afterburner-Wert, obwohl
der Rest der Prozessorquelle aus Windows kommt.

Die Frage, die das Panel beantwortet, ist: **was verliere ich, wenn ich dieses
Programm schließe.**

Die Herkunft erreicht **keinen** Export. Sie steht nicht in JSON, Prometheus
oder InfluxDB, denn sonst könnte ein Dashboard davon abhängen, welche
Hilfsprogramme auf einer Maschine zufällig laufen — das Gegenteil der Zusage,
dass derselbe Messwert aus jeder Quelle gleich aussieht.


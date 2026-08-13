# Spielerkennung

RTSS meldet die ausführbare Datei. `Cyberpunk2077.exe` veröffentlicht der
Messwert **Spiel** seit jeher, und mehr als ein Dateiname ist das nicht — die
Schreibweise, die irgendein Build-System gewählt hat. Das Spiel heißt
*Cyberpunk 2077*, und sein Titelbild wird über keinen dieser beiden Namen
adressiert, sondern über eine Zahl: die Steam-AppID `1091500`.

Nichts macht aus dem einen das andere. `SpaceMarine2.exe` ist *Warhammer 40,000:
Space Marine 2*, `bl3.exe` ist *Borderlands 3*, `DOOM64.exe` ist *DOOM 64* mit
einem Leerzeichen. Dateinamen zerlegen, Ziffern abschneiden, Großbuchstaben
einsetzen — so raffiniert die Regel auch ist, das Ergebnis bleibt geraten, und
geraten ist genau das, was hier nicht veröffentlicht werden darf. Die Antwort
muss nachgeschlagen werden, und die Einzigen, die sie haben, sind der Launcher,
der das Spiel installiert hat, und der Store, der es verkauft.

**Versuche Gamename und SteamID zu ermitteln** auf der Seite
[Datengewinnung](interface/data-capture.md#das-spiel-ermitteln) ist dieses
Nachschlagen. Die Option ist standardmäßig aus, sie trägt die Kennzeichnung
`ALPHA · Internet`, und sie ist die einzige Einstellung in diesem Programm, die
den Server eines Dritten kontaktiert.

## Drei Quellen, die billigste zuerst

1. **Steam, über die Registry.** Steam schreibt die gestartete App nach
   `HKCU\Software\Valve\Steam\RunningAppID` und den Titel, den es dazu führt,
   nach `…\Steam\Apps\<id>\Name`. Zwei Lesezugriffe: keine Elevation, kein
   Zugriff auf den Prozess des Spiels, nichts, was den Rechner verlässt. Bei
   einem Steam-Spiel ist das die ganze Antwort — Titel und AppID kommen
   zusammen.
2. **Die Kataloge von GOG und Epic, auf der Platte.** Keiner der beiden
   Launcher sagt, was *läuft*; beide sagen, was *installiert* ist und wo. Also
   wird die Frage umgedreht: der von RTSS gemeldete Pfad wird gegen die
   Installationsordner gehalten, die GOG in der Registry und Epic als
   Manifestdateien führt. Der längste passende Ordner gewinnt — ein Spiel im
   Verzeichnis eines anderen Spiels wird damit als es selbst gemeldet und nicht
   als sein Wirt.
3. **Die öffentliche Steam-Suche.** Sie wird in zwei Fällen gefragt: nach der
   AppID zu einem Titel, den Schritt 2 benannt hat — und, wenn keiner der
   Schritte davor überhaupt etwas benannt hat, nach einem Suchbegriff, der aus
   der ausführbaren Datei selbst gebildet wird. Aus `Cyberpunk2077.exe` wird die
   Frage „Cyberpunk 2077", aus `MyGame-Win64-Shipping.exe` wird „My Game". Das
   ist die einzige Vermutung im ganzen Verfahren, und sie betrifft nur, was
   **gefragt** wird, nie, was gemeldet wird: veröffentlicht wird der Titel, mit
   dem der Store antwortet, und die AppID, die er nennt — oder gar nichts. Ein
   Spiel, das außerhalb dieser drei Läden gekauft wurde, hätte sonst keinerlei
   Aussicht auf eine AppID, und die AppID ist das, was das Bild holt.

   Programme, die nie ein Spiel sind — Browser, Chatfenster, Aufnahmewerkzeuge,
   die Launcher selbst — werden gar nicht erst gefragt. RTSS hakt sich in alles
   ein, was Bilder ausgibt, also tauchen sie hier genauso auf wie ein Spiel, und
   „Origin" käme als ein Spiel namens Origin zurück.

Die Reihenfolge ist eine Kostenleiter, die zugleich eine Verlässlichkeitsleiter
ist. Steams Antwort ist die eigene Aufzeichnung des Launchers darüber, was er
gestartet hat — genau, lokal, umsonst. GOG und Epic kosten eine
Registry-Aufzählung und ein Verzeichnis kleiner Dateien, und ihre Antwort ist
ein Pfadabgleich: richtig, solange ein Spiel nicht an einer sehr merkwürdigen
Stelle liegt. Die Antwort des Stores ist eine *Suche*, der beste Treffer zu
einem Suchbegriff, und sie ist die einzige der drei, die falsch sein kann — und
die einzige, die den Rechner verlässt. Die billigste zuerst heißt deshalb auch:
die fehlbare zuletzt, und nur dann, wenn die anderen nichts zu sagen haben.

Ein Pfad, zu dem sich kein Katalog bekennt, ist ein erneutes Einlesen wert,
höchstens alle fünf Minuten — so sieht ein Spiel aus, das installiert wurde,
während das Programm lief, und „ich habe es installiert, warum ist es nicht da"
ist eine Meldung, mit der niemand etwas anfangen kann. Eine ausführbare Datei,
zu der die Antwort schon feststeht, kommt dort nie an.

Zwei weitere Wege, Steam zu fragen, wurden gemessen und verworfen; die Gründe
stehen unter [Wie die Werte zustande kommen](how-values-are-obtained.md).

## Erweiterungen liegen im Ordner des Hauptspiels

Das ist die Falle, aus der das falsche Bild entsteht, und es ist gut zu wissen,
dass sie behandelt und nicht übersehen wurde.

GOG und Epic führen eine Erweiterung im selben Katalog wie das Spiel, das sie
erweitert, und zwar mit demselben Ordner. Auf dem Rechner, auf dem das entstanden
ist, nennen drei GOG-Einträge das Verzeichnis von *Cyberpunk 2077*: das Spiel,
*Phantom Liberty* und *REDmod*. Wer einen Pfad naiv gegen diese Liste hält,
bekommt irgendeinen der drei — und die Folge ist kein fehlendes Symbol. Schickt
man „Cyberpunk 2077: Phantom Liberty" an die Store-Suche, antwortet sie mit der
AppID der Erweiterung, und Home Assistant zeigt das dann mit voller Überzeugung
an.

Jeder Launcher kennzeichnet den Unterschied auf seine eigene Weise. Beides ist
an echten Installationen abgelesen und nicht angenommen:

| Launcher | Woran eine Erweiterung zu erkennen ist |
|---|---|
| **GOG** | sie trägt `dependsOn` mit dem Spiel, das sie erweitert, und hat keine eigene `exe` |
| **Epic** | sie hat kein `LaunchExecutable`. `MainGameAppName` sieht nach dem naheliegenden Test aus und ist keiner — *DOOM 64* lässt es leer und ist sehr wohl ein Spiel |

Nur Einträge, die diese Prüfung bestehen, erreichen den Pfadabgleich überhaupt.

## Was den Rechner verlässt — und was nicht

Schritt 1 und 2 sind eine Handvoll Registry-Zugriffe und ein paar kleine
Dateien. Schritt 3 ist eine HTTPS-Anfrage an die öffentliche Steam-Suche —
dasselbe Ziel, das auch das Suchfeld auf der Store-Seite anspricht, ohne
Schlüssel, ohne Konto, ohne Anmeldung. Hinaus geht der Titel des Spiels — oder,
wenn ihn kein Launcher benannt hat, ein Suchbegriff aus dem Namen der
ausführbaren Datei, also aus einem Namen, den man selbst installiert hat. Sonst
nichts: kein Rechnername, keine Hardware, keine Konfiguration, keinerlei
Kennung. Zurück kommen eine AppID und die Schreibweise, die der Store dazu führt.

Gefragt wird **einmal je Suchbegriff**, und die Antwort bleibt gemerkt — **auch wenn
sie leer war**. Ein Spiel, das der Store nicht kennt, ist genau der Fall, der
sonst bei jeder Messung erneut fragen würde, zweimal pro Sekunde, solange das
Spiel läuft.

Gewartet wird nie. Die Anfrage läuft neben der Messschleife; ein Titel, dessen
AppID noch nicht da ist, wird ohne sie veröffentlicht und bekommt sie bei einer
späteren Messung. Ein langsamer Store darf keinen langsamen Exporter ergeben.

Gemerkt wird ausschließlich im Speicher. Auf die Platte wird nichts geschrieben;
ein Neustart des Programms — oder eine geänderte Einstellung, die den Collector
neu baut — vergisst alles Gelernte, und das ist zugleich der einzige Weg, es zu
leeren.

**Ein Schalter, nicht drei.** Ist die Option aus, wird gar nichts ermittelt:
weder über den Store noch über die beiden lokalen Quellen. Der Messwert **Spiel**
ist dann die ausführbare Datei und sonst nichts, genau wie vor dieser Funktion.

## Fehlt statt geraten

Eine ausführbare Datei, die auch der Store nicht erkennt, erzeugt gar keine
Details. Ein Spiel, das der Store nicht hat, behält Plattform und Titel und hat
schlicht keine AppID. Und ein Spiel, das allein über den Namen gefunden wurde,
hat Titel und AppID, aber **keine Plattform** — kein Launcher hat sich dazu
bekannt, also gibt es dort nichts zu melden. Es gibt hier keine leeren
Zeichenketten, keine Nullen und kein „unbekannt": dieselbe Regel wie überall
sonst im Programm, denn ein Wert, der dasteht, behauptet etwas, ein fehlender
nicht.

Bei der AppID ist die Regel mehr als eine Vorliebe. Eine falsche AppID ist kein
fehlendes Bild — sie ist das Bild des falschen Spiels, und kein Bild ist besser
als das.

## Auf einem Home-Assistant-Dashboard

Die Entity **Spiel** bleibt unangetastet: ihr Zustand ist weiterhin die
ausführbare Datei, ihre Entity-ID bleibt dieselbe, und jede darauf gebaute
Automatisierung funktioniert weiter. Plattform, Titel und AppID kommen als
**Attribute** derselben Entity dazu. Wie die Nachricht aussieht, steht unter
[Exportziele](export-targets.md#spiel-attribute).

| Attribut | Beispiel | Was es ist |
|---|---|---|
| `platform` | `steam`, `gog`, `epic` | ein Bezeichner, klein geschrieben, nie übersetzt |
| `title` | `Cyberpunk 2077` | so, wie der Store ihn schreibt, samt Zeichensetzung |
| `app_id` | `1091500` | Steams Kennung des Titels — sie adressiert das Bildmaterial |

Steam liefert das Bildmaterial zu einer AppID direkt aus seinem CDN:

```
https://cdn.cloudflare.steamstatic.com/steam/apps/<appid>/header.jpg
```

Fünf Dateien sind brauchbar, und welche man nimmt, hängt von der Form des
Platzes auf dem Dashboard ab:

| Datei | Was es ist |
|---|---|
| `header.jpg` | das breite Banner, die übliche Wahl für eine Karte |
| `capsule_231x87.jpg` | die kleine breite Kapsel, für einen kachelgroßen Platz |
| `library_600x900.jpg` | das hochkante Cover, die Form eines Bibliotheksrasters |
| `library_hero.jpg` | der breite Hintergrund, für eine Seite oder Kartenfläche |
| `logo.png` | nur der Schriftzug, transparent, zum Auflegen auf den Hintergrund |

Gemessen an `1091500`: alle fünf antworten über schlichtes HTTPS, ohne
Schlüssel, ohne Konto, ohne Cookie.

Eine Markdown-Karte reicht, um eines davon zu zeigen, und braucht nichts aus
HACS:

```yaml
type: markdown
entity_id:
  - sensor.re_corganpc2_game
content: |
  {% set id = state_attr('sensor.re_corganpc2_game','app_id') %}
  {% if id %}![](https://cdn.cloudflare.steamstatic.com/steam/apps/{{ id }}/header.jpg)

  **{{ state_attr('sensor.re_corganpc2_game','title') }}**{% else %}_nichts erkannt_{% endif %}
```

Das `{% if id %}` ist keine Höflichkeit. Die Attribute werden geleert, sobald
nichts mehr erkannt wird — das ist es, was das Cover eines beendeten Spiels vom
Dashboard nimmt —, und eine Karte, die das Attribut voraussetzt, würde
stattdessen ein kaputtes Bild anzeigen. Weitere Karten stehen unter
[Home Assistant](home-assistant.md#karten-konfiguration).

## Was Alpha hier heißt

Gelesen werden drei Launcher: Steam, GOG Galaxy und Epic. Alle anderen —
Ubisoft Connect, EA app, Battle.net, itch.io, ein von Hand installiertes Spiel —
nicht, und ein Spiel von dort bleibt eine ausführbare Datei.

**Die Launcher-Kataloge sind auf einem einzigen Rechner gelesen worden.** Eine
Steam-Installation, ein GOG Galaxy, ein Epic-Launcher, mit den Spielen, die
zufällig darauf lagen. Die Tests, die ein Spiel von einer Erweiterung trennen,
und die Felder, aus denen die Ordner gelesen werden, sind das, was in diesen
Installationen tatsächlich steht; ein anderer Rechner schreibt vielleicht etwas
anders oder führt ein Feld, mit dem hier niemand rechnet. Genau das braucht
fremde PCs, bevor es aufhört, Alpha zu sein.

Zwei bekannte Grenzen kommen dazu:

* **Die Antwort des Stores ist sein bester Treffer**, keine Gewissheit. Ein
  Titel, dem eine Edition, ein Bündel oder eine Neuauflage im Weg steht, kann
  auf einer benachbarten AppID landen.
* **Steam meldet die gestartete App, nicht das Fenster im Vordergrund.** Ein
  Steam-Spiel, das im Hintergrund weiterläuft, sticht damit das, was RTSS gerade
  zeichnet. Das ist ein seltener Zustand, und die Alternative — aus dem Pfad zu
  raten, welches von zwei laufenden Spielen gemeint ist — wäre schlechter.

Was hier falsch liegt, liegt in einem **Attribut** falsch. Zustand, Identität
und Verlauf der Entity **Spiel** bleiben davon unberührt.

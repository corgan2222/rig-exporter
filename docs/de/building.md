# Selbst bauen

Sie brauchen dafür kein fertiges Release: der Quelltext baut auf einem
gewöhnlichen Windows-Rechner in unter einer Minute, und es fällt genau eine
Datei dabei ab.

## Was Sie brauchen

| | |
|---|---|
| **Go** | 1.26 oder neuer. `go.mod` verlangt 1.26.5, die Abhängigkeiten für sich genommen 1.25 |
| **Git** | für die Buildkennung — ohne Git baut es auch, nur ohne Kennung |
| **C-Compiler** | **nein.** Kein CGO, keine Bibliothek, kein Toolchain-Zoo |

Zielplattform ist `windows/amd64`. Andere Windows-Architekturen übersetzen, sind
aber nie gelaufen; außerhalb von Windows übersetzt gar nichts — das ganze
Programm steht hinter `//go:build windows`.

## Bauen

```powershell
.\build.ps1
```

Ergebnis ist ein einzelnes `rig-exporter.exe` von rund 18 MB, ohne
Begleitdateien. Gebaut wird mit `-H windowsgui -s -w`: kein Konsolenfenster,
keine Debug-Symbole.

!!! warning "`go build` allein reicht nicht"

    Ohne `-H windowsgui` entsteht ein Konsolen-Binary. Das startet, sieht
    lauffähig aus und verhält sich trotzdem anders als das ausgelieferte
    Programm — `-probe` besonders. Wenn Sie von Hand bauen, setzen Sie den
    Schalter.

## Prüfen

```powershell
.\build.ps1 -Check
```

Das ist derselbe Lauf, den die CI auf jedem Pull Request ausführt, und er ist
die Antwort auf „läuft das durch": `gofmt`, `go vet -unsafeptr=false`,
`staticcheck` mit allen 149 Prüfungen aus `staticcheck.conf`, sämtliche Tests —
und erst danach der Build. Ein roter Prüflauf baut nichts.

```powershell
.\build.ps1 -Check -Race
```

Dasselbe zusätzlich unter dem Race Detector. Der läuft getrennt, weil er als
einziger Teil des Projekts **cgo** braucht und die Laufzeit ungefähr verdoppelt.
Das Skript sucht sich einen mingw-w64 unter `C:\msys64\ucrt64` oder
`…\mingw64` selbst.

!!! danger "Nicht `C:\msys64\usr\bin\gcc`"

    Der baut gegen die MSYS-Runtime und ist für Go unbrauchbar. `CGO_ENABLED`
    wird ohnehin nur für den Testlauf gesetzt und danach zurückgenommen — das
    ausgelieferte Binary bleibt cgo-frei.

Und was ein grüner Lauf **nicht** heißt: dass der Code frei von Races ist. Er
heißt, dass die vorhandenen Tests keines ausgelöst haben. Ein Detektor findet
nur, was auch wirklich passiert.

## Die Buildkennung

Hinter der Version steht, woher das Binary kommt:

```
rig-exporter 1.10.3+<commits>.<hash>
```

Commit-Anzahl aus `git rev-list --count`, Kurz-Hash aus `git rev-parse --short`,
und bei uncommitteten Änderungen zusätzlich `.dirty`. Abgeleitet statt gepflegt,
damit sie von dem Code, den sie beschreibt, gar nicht abweichen kann.

Der Grund: eine Versionsnummer allein beantwortet nie die Frage „ist das das
Binary mit der Korrektur" — zwischen zwei Commits bewegt sie sich nicht. Ein
schlichtes `go build` lässt die Kennung leer, und auch das ist eine ehrliche
Auskunft: dieses Binary kam nicht aus dem Skript.

Nebenwirkung, über die man einmal stolpert: **jede uncommittete Datei markiert
den Build als `.dirty`** — auch eine, die nur herumliegt.

## Das Symbol

```powershell
.\build.ps1 -Icon
```

Nur damit werden die Icons neu erzeugt; sonst liegen sie fertig im Repository.
`tools/genicon` macht aus **einer** Quelle — `docs/images/rig-exporter-entity-512.png` —
drei Dinge:

* `icon.ico` für den Infobereich
* `rsrc_windows_amd64.syso`, die Windows-Ressourcendatei, die der Exe ihr Symbol
  in Explorer, Taskleiste und Alt-Tab gibt
* `icon.png`, das die Weboberfläche unter `/icon.png` ausliefert

Drei Bilder desselben Programms, die sich widersprechen, wären schlimmer als
keins.

Dass das ein eigener Schalter ist und nicht bei jedem Build mitläuft, hat einen
handfesten Grund: `go run` linkt dafür ein frisches, unsigniertes Binary in den
Build-Cache und startet es von dort — genau das Muster, das Microsoft Defender
heuristisch als `Trojan:Win32/Sabsik` meldet. Ein Virenfund bei jedem Prüflauf
ist den Schreck nicht wert.

Die Ressourcendatei wird übrigens von Hand geschrieben statt mit `rsrc` oder
`goversioninfo` erzeugt, und alle drei Dateien sind eingecheckt — damit ein
blankes `go build` ohne jedes zusätzliche Werkzeug reicht.

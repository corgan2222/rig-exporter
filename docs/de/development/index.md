# Entwicklung

Für alle, die den Quelltext anfassen wollen — oder auch nur wissen möchten, was
beim Bauen eigentlich passiert.

<div class="grid cards" markdown>

-   :material-hammer-wrench: **[Selbst bauen](../building.md)**

    Go, ein Skript, kein C-Compiler. Was `-Check`, `-Race` und `-Icon` tun und
    warum das drei getrennte Schalter sind.

-   :material-plus-box-outline: **[Eigene Messwerte hinzufügen](../custom-measurements.md)**

    Einen Wert melden, den das Programm noch nicht kennt. Vier Schritte, und
    keine Zeile Export-Code.

</div>

Das Repository liegt auf [GitHub](https://github.com/corgan2222/rig-exporter).
Jede Änderung geht über einen Pull Request, und die CI führt denselben
`build.ps1 -Check` aus, den Sie lokal ausführen können — es gibt also keine
Überraschung, die erst auf dem Server auftaucht.

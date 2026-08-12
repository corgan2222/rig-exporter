<div class="hero" markdown>

# ![](images/rig-exporter-mark.svg) rig-exporter

Telemetrie eines Gaming-PCs für Home Assistant, Prometheus und InfluxDB.
Ein einzelnes Windows-Programm, rund 18 MB, ohne Installation und ohne
Abhängigkeiten.

[In fünf Minuten starten](getting-started/index.md){ .md-button .md-button--primary }
[Auf GitHub](https://github.com/corgan2222/rig-exporter){ .md-button }

</div>

Es liest, was der Rechner über sich selbst weiß — Bilder pro Sekunde,
Temperaturen, Auslastung, freier Platz, Durchsatz, Akku — und schickt es
dorthin, wo Sie es sehen wollen.

![Die Anzeigeseite von rig-exporter](images/screenshots/de/dashboard.png)

<div class="grid cards" markdown>

-   :material-home-assistant: **Home Assistant ohne Handarbeit**

    MQTT-Discovery legt die Entitäten selbst an, mit Gerät, Symbolen und
    Einheiten. Nichts in `configuration.yaml` einzutragen.

-   :material-chart-line: **Prometheus und InfluxDB**

    Dieselben Werte als Text-Exposition und als Line Protocol — abholbar oder
    von selbst geschrieben.

-   :material-tune: **Sie entscheiden, was gesendet wird**

    122 Messwerte im Katalog, jeder einzeln abwählbar. Drei Voreinstellungen
    von „nur die Kacheln" bis „alles".

-   :material-eye-off: **Nur was wirklich da ist**

    Kein Akku, keine Akku-Werte. Ein fehlender Wert wird weggelassen, nicht
    als Null erfunden.

</div>

## In fünf Minuten

1. Die `rig-exporter.exe` vom [Release](https://github.com/corgan2222/rig-exporter/releases)
   herunterladen und starten. Es öffnet sich die Oberfläche auf
   `http://127.0.0.1:8787`.
2. Unter [**Export & Anzeige → MQTT**](interface/export-and-display.md#mqtt-push-an-home-assistant)
   den MQTT-Broker eintragen.
3. Fertig. Home Assistant zeigt das Gerät nach wenigen Sekunden.

Alles Weitere — welche Messwerte es gibt, was Sie dafür installiert haben
müssen, wie die Werte zustande kommen — steht in diesem Handbuch.

## Wo anfangen

- [Was gemeldet wird](what-is-reported.md) — der volle Messwertkatalog, nach
  Gruppen sortiert
- [Voraussetzungen](requirements.md) — was ohne Zusatzprogramm geht und was
  nicht
- [Erster Start](first-run.md) — Autostart, Konfigurationsdatei, Sprache
- [Oberfläche](interface/index.md) — die vier Seiten und was darauf steht

!!! info "Nur Windows"

    rig-exporter liest Schnittstellen, die es nur unter Windows gibt: DXGI, die
    WDDM-Leistungsindikatoren, RTSS' Shared Memory. Eine Linux-Fassung ist
    nicht geplant, weil dort nichts davon existiert.

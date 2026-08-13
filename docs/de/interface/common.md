# Was für alle Seiten gilt

Auf den beiden Seiten mit Formularen — *Datengewinnung* und
*Export & Anzeige* — hat jeder Kasten mit Einstellungen einen eigenen
Speichern-Button, der erst grün wird, wenn in genau diesem Kasten etwas
geändert wurde. Gespeichert wird auch nur dieser eine Kasten: ein Formular
trägt keinen Beleg über Kästchen, die es gar nicht enthält, und eine
Teilübernahme würde sonst alles auf der anderen Seite abschalten. *Messwerte*
hat gar keinen Speichern-Button: dort wirkt jede Änderung sofort an der Stelle,
an der sie gemacht wird, und die Kopfzeile bestätigt sie kurz mit „übernommen".

Jeder Kasten lässt sich über seine Überschrift **zuklappen**, und die Seite
merkt sich, was zu war — auch über einen Neustart. Das liegt bewusst im Browser
und nicht in der Konfiguration: geht der eingestellte Port verloren, weicht die
Oberfläche auf einen zufälligen aus, und ein anderer Port ist eine andere
Herkunft, an der der Speicher hängt. Eine Ansichtsvorliebe darf dabei verloren
gehen; die Antwort auf eine Frage nicht, die steht in der Konfiguration. Ein
Link auf einen zugeklappten Kasten klappt ihn wieder auf.

Rechts in der Kopfzeile steht der Sprachumschalter. Er wirkt auf Oberfläche,
Tray-Menü, Dialoge und die angezeigten Entity-Namen in Home Assistant. Was er
ausdrücklich **nicht** anfasst, sind die Kennungen: `default_entity_id`,
`object_id`, `unique_id` und die Wertvorlage bleiben gleich, weil Dashboards und
Automatisierungen daran hängen. Eine Entity-ID wie `sensor.re_corganpc2_fps`
heißt in beiden Sprachen gleich, nur der angezeigte Name wechselt. Dashboards
und Automationen überleben einen Sprachwechsel also unbeschadet.

Nicht übersetzt wird, was Maschinen lesen: Prometheus-Hilfetexte, Logzeilen und
Fehlermeldungen bleiben englisch. Dasselbe gilt für gemeldete *Werte* —
`Ethernet`, `Wi-Fi`, `Other`, `DDR4`, `Type 126` bleiben englisch, damit eine
Automatisierung nicht davon abhängt, welche Sprache gerade eingestellt ist.

Unten auf jeder Seite öffnen drei Schaltflächen die Konfiguration, das Log und
den Ordner darum. Der Umweg über den Server ist nötig, weil ein Browser einem
`file://`-Link von einer `http`-Seite aus nicht folgt.

Daneben öffnet **Hilfe** dieses Handbuch — in der Sprache, auf die die
Oberfläche gerade eingestellt ist, eine deutsche Oberfläche landet also auf den
deutschen Seiten. Es ist ein Link und keine Schaltfläche, weil er den Rechner
verlässt, und dieser Unterschied gehört in die Statuszeile des Browsers vor den
Klick statt hinter ihn.


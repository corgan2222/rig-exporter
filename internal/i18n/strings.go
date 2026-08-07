package i18n

// catalogue holds the interface text. Keys are dotted paths that mirror where
// the string appears, so a key read in a template says where to find it.
var catalogue = map[string]Text{
	// Navigation and page chrome.
	"nav.status":        {DE: "Anzeige", EN: "Dashboard"},
	"nav.capture":       {DE: "Datengewinnung", EN: "Data capture"},
	"nav.export":        {DE: "Export & Anzeige", EN: "Export & display"},
	"nav.settings":      {DE: "Einstellungen", EN: "Settings"},
	"page.status":       {DE: "Anzeige", EN: "Dashboard"},
	"page.capture":      {DE: "Datengewinnung", EN: "Data capture"},
	"nav.measurements":  {DE: "Messwerte", EN: "Measurements"},
	"page.measurements": {DE: "Messwerte", EN: "Measurements"},

	// The measurement selection page.
	"measurements.title":      {DE: "Umfang", EN: "Scope"},
	"measurements.all":        {DE: "alle", EN: "all"},
	"measurements.none":       {DE: "keine", EN: "none"},
	"measurements.selectedOf": {DE: "%1 von %2", EN: "%1 of %2"},
	"measurements.entities":   {DE: "Entities", EN: "entities"},
	"measurements.pollNote":   {DE: "ohne Einfluss auf die Menge", EN: "does not change the amount"},
	"measurements.applied":    {DE: "übernommen", EN: "applied"},
	"measurements.keepOut": {
		DE: "Ein Messwert muss nicht in der Datenbank landen. Der Recorder kann eine Entity zeigen, ohne ihren Verlauf aufzuzeichnen — das kostet keine Zeile.",
		EN: "A measurement need not reach the database. The recorder can show an entity without keeping its history, and that costs no rows at all.",
	},
	"measurements.decimalsChanged": {
		DE: "Die Änderungsrate oben wurde mit der bisherigen Einstellung gemessen. Nach dem Speichern misst der Exporter neu — die Schätzung stimmt bis dahin nicht.",
		EN: "The change rate above was measured with the previous setting. The exporter measures again once this is saved; until then the estimate does not apply.",
	},
	"measurements.rowsPerDay": {DE: "DB-Einträge pro Tag", EN: "database rows per day"},
	"measurements.dbSize":     {DE: "Datenbank nach der Aufbewahrungszeit", EN: "database after the retention period"},
	"measurements.rungNote": {
		DE: "Loslassen wählt die Stufe %1 und verwirft die einzeln gesetzten Haken.",
		EN: "Releasing picks the %1 rung and discards the individually ticked boxes.",
	},
	// The assumptions belong next to the number, not behind a tooltip: the
	// result is worth nothing without them, and somebody has to be able to see
	// which one is wrong on their machine.
	"measurements.basis": {
		DE: "Grob überschlagen: %1, alle %2 s gesendet, 300 Byte je Zeile angenommen, %4 Tage Aufbewahrung. Home Assistant speichert bei Änderung, nicht beim Senden — die Nachkommastellen sind hier der größte Hebel.",
		EN: "Roughly estimated: %1, published every %2 s, 300 bytes per row assumed, %4 days of retention. Home Assistant stores on change, not on publish — the decimals are the biggest lever here.",
	},
	"measurements.measured": {
		DE: "%1 % der Werte ändern sich je Sendung (gemessen über %2 Sendungen)",
		EN: "%1 % of the values change per publish (measured over %2 publishes)",
	},
	"measurements.assumed": {
		DE: "%1 % der Werte ändern sich je Sendung (angenommen, noch nichts gemessen)",
		EN: "%1 % of the values change per publish (assumed, nothing measured yet)",
	},
	"page.export": {DE: "Export & Anzeige", EN: "Export & display"},

	"footer.by":      {DE: "von", EN: "by"},
	"footer.project": {DE: "Projektseite auf GitHub", EN: "Project page on GitHub"},
	"footer.config":  {DE: "Konfiguration öffnen", EN: "Open configuration"},
	"footer.log":     {DE: "Log öffnen", EN: "Open log"},
	"footer.folder":  {DE: "Ordner öffnen", EN: "Open folder"},

	// Status page.
	"status.title":      {DE: "Status", EN: "Status"},
	"status.fps":        {DE: "FPS", EN: "FPS"},
	"status.frametime":  {DE: "Frametime", EN: "Frame time"},
	"status.game":       {DE: "Spiel", EN: "Game"},
	"status.resolution": {DE: "Auflösung", EN: "Resolution"},
	"status.cpu":        {DE: "CPU", EN: "CPU"},
	"status.ram":        {DE: "RAM", EN: "RAM"},
	"status.noGame":     {DE: "kein Spiel", EN: "no game"},
	"status.pause":      {DE: "Pause", EN: "Pause"},
	"status.resume":     {DE: "Fortsetzen", EN: "Resume"},
	"status.exporting":  {DE: "Export aktiv", EN: "Exporting"},
	"status.paused":     {DE: "Export pausiert", EN: "Export paused"},
	"status.noExport":   {DE: "Kein Export aktiv", EN: "No export target active"},
	"status.offline":    {DE: "Oberfläche getrennt", EN: "Interface disconnected"},
	"status.updated":    {DE: "Aktualisiert", EN: "Updated"},

	"rtss.unavailable": {DE: "RTSS nicht verfügbar", EN: "RTSS unavailable"},
	"rtss.connected":   {DE: "verbunden", EN: "connected"},
	// Both programs are named, and both are linked. RTSS supplies the frame
	// rate and Afterburner the temperatures, so somebody looking at an empty
	// FPS tile is usually missing both — and pointing at only one of them
	// sends them back a second time.
	"rtss.bannerTitle": {
		DE: "MSI Afterburner oder RivaTuner Statistics Server läuft nicht.",
		EN: "MSI Afterburner or RivaTuner Statistics Server is not running.",
	},
	"rtss.bannerBody": {
		DE: "Ohne RTSS gibt es keine FPS-Werte, ohne Afterburner keine Temperaturen — alle übrigen Gruppen melden weiter.",
		EN: "Without RTSS there are no FPS values, without Afterburner no temperatures; every other group keeps reporting.",
	},
	// An AMD card changes what is actually missing. With its driver answering,
	// temperature, clocks, fan and power are already there and only the frame
	// rate is not — naming Afterburner then sends somebody after a program they
	// do not need, which is the same mistake as naming only one of the two.
	"rtss.bannerTitleAMD": {
		DE: "RivaTuner Statistics Server läuft nicht.",
		EN: "RivaTuner Statistics Server is not running.",
	},
	//
	// It must not depend on whether something is presenting at this instant.
	// The driver only reports a frame rate while a fullscreen application is
	// drawing, so a text keyed on the current reading would alternate between
	// "there are no FPS" and "here are your FPS" on an unchanged machine — and
	// the first of those is simply untrue here.
	"rtss.bannerBodyAMD": {
		DE: "Temperatur, Takt, Lüfter und Leistung liefert der AMD-Treiber bereits, und im Vollbild zählt er auch die Bildrate. Ohne RTSS fehlen nur der Fenstermodus und der Name des laufenden Spiels.",
		EN: "Temperature, clocks, fan and power already come from the AMD driver, and in fullscreen it counts the frame rate too. Without RTSS only windowed mode and the name of the running game are missing.",
	},
	// The other AMD case: the card is present but its driver says nothing. That
	// is what a display-driver-only installation looks like, and the remedy is
	// the full Adrenalin package rather than another monitoring program.
	"rtss.bannerBodyAMDDriver": {
		DE: "Ohne RTSS gibt es keine FPS-Werte. Die AMD-Karte meldet zurzeit auch keine Temperaturen — dafür braucht es das vollständige Adrenalin-Paket, nicht nur den Anzeigetreiber.",
		EN: "Without RTSS there are no FPS values. The AMD card is not reporting temperatures either — that needs the full Adrenalin package, not just the display driver.",
	},
	"rtss.downloadAMD":         {DE: "AMD-Treiber herunterladen", EN: "Download AMD driver"},
	"rtss.downloadAfterburner": {DE: "MSI Afterburner herunterladen", EN: "Download MSI Afterburner"},
	"rtss.download":            {DE: "RTSS herunterladen", EN: "Download RTSS"},
	"rtss.alsoInAfterburner":   {DE: "(in Afterburner bereits enthalten)", EN: "(already included with Afterburner)"},
	// Two labels for one button. It sets the same switch either way, but the
	// reason differs: a machine with no graphics card at all cannot have game
	// data, while a machine with one simply is not used for games. Claiming
	// "no GPU present" on a Radeon is plainly wrong, and wrong text in an
	// interface costs more trust than a missing feature.
	"rtss.dismissNoGPU":  {DE: "Keine GPU vorhanden — Spieldaten ausblenden", EN: "No GPU present — hide game status"},
	"rtss.dismissNoGame": {DE: "Kein Spielrechner — Spieldaten ausblenden", EN: "Not used for gaming — hide game status"},

	// The update box. Only ever on screen when there is something newer, so
	// none of these has to cope with "you are up to date".
	"update.title":     {DE: "Neue Version verfügbar:", EN: "A new version is available:"},
	"update.installed": {DE: "Installiert ist %s.", EN: "Installed is %s."},
	"update.notes":     {DE: "Was sich geändert hat", EN: "What changed"},
	"update.install":   {DE: "Jetzt aktualisieren", EN: "Update now"},
	"update.running":   {DE: "Wird installiert …", EN: "Installing …"},
	"update.installHint": {
		DE: "Wird heruntergeladen, die Signatur geprüft und danach neu gestartet. Dauert ein paar Sekunden.",
		EN: "Downloaded, signature-checked, then restarted. It takes a few seconds.",
	},

	// Hardware panels.
	"hardware.title":    {DE: "Hardware", EN: "Hardware"},
	"hardware.disabled": {DE: "Abgeschaltet.", EN: "Switched off."},
	"hardware.noData":   {DE: "Keine Daten", EN: "No data"},
	"hardware.none":     {DE: "Keine Sensorgruppen aktiv.", EN: "No sensor groups are active."},
	"hardware.byMetric": {DE: "Nach Messwert", EN: "By measurement"},
	"hardware.byDevice": {DE: "Nach Gerät", EN: "By device"},

	// Endpoint list.
	"endpoints.title":      {DE: "Endpunkte", EN: "Endpoints"},
	"endpoints.json":       {DE: "JSON (Home Assistant RESTful)", EN: "JSON (Home Assistant RESTful)"},
	"endpoints.prometheus": {DE: "Prometheus", EN: "Prometheus"},
	"endpoints.influx":     {DE: "InfluxDB Line Protocol", EN: "InfluxDB line protocol"},
	"endpoints.tokenNote": {
		DE: "Zugriff nur mit Token: Header <code>Authorization: Bearer &lt;token&gt;</code> oder <code>?token=&lt;token&gt;</code>.",
		EN: "A token is required: header <code>Authorization: Bearer &lt;token&gt;</code> or <code>?token=&lt;token&gt;</code>.",
	},
	"endpoints.hint": {
		DE: "Der Datenserver ist abgeschaltet. Einschalten unter Einstellungen → Datenserver.",
		EN: "The data server is switched off. Turn it on under Settings → Data server.",
	},

	// Settings, shared.
	"settings.saved":      {DE: "Einstellungen gespeichert und übernommen.", EN: "Settings saved and applied."},
	"settings.failed":     {DE: "Speichern fehlgeschlagen", EN: "Saving failed"},
	"settings.save":       {DE: "Speichern & übernehmen", EN: "Save and apply"},
	"settings.saveHint":   {DE: "Betroffene Verbindungen werden bei Bedarf neu aufgebaut.", EN: "Affected connections are rebuilt where needed."},
	"settings.keepSecret": {DE: "gespeichert – leer lassen zum Behalten", EN: "stored — leave blank to keep"},
	"settings.noSecret":   {DE: "nicht gesetzt", EN: "not set"},
	"settings.jumpTo":     {DE: "Direkt zu", EN: "Jump to"},
	"settings.unchanged":  {DE: "keine Änderungen", EN: "no changes"},
	"settings.unsaved":    {DE: "ungespeicherte Änderungen", EN: "unsaved changes"},

	// Settings, MQTT.
	"settings.mqtt.title":     {DE: "MQTT — Push an Home Assistant", EN: "MQTT — push to Home Assistant"},
	"settings.mqtt.nav":       {DE: "MQTT", EN: "MQTT"},
	"settings.mqtt.enabled":   {DE: "MQTT-Export aktiv (Autodiscovery in Home Assistant)", EN: "MQTT export active (autodiscovery in Home Assistant)"},
	"settings.mqtt.host":      {DE: "Host", EN: "Host"},
	"settings.mqtt.port":      {DE: "Port", EN: "Port"},
	"settings.mqtt.username":  {DE: "Benutzername", EN: "User name"},
	"settings.mqtt.password":  {DE: "Passwort", EN: "Password"},
	"settings.mqtt.clearPass": {DE: "Gespeichertes Passwort löschen", EN: "Delete the stored password"},
	"settings.mqtt.tls":       {DE: "TLS verwenden (Port meist 8883)", EN: "Use TLS (usually port 8883)"},
	"settings.mqtt.tlsSkip":   {DE: "Zertifikat nicht prüfen (nur für selbstsignierte Broker)", EN: "Skip certificate checks (self-signed brokers only)"},
	"settings.mqtt.clientId":  {DE: "Client-ID", EN: "Client ID"},

	// Settings, Home Assistant identity.
	"settings.ha.title":         {DE: "Home Assistant", EN: "Home Assistant"},
	"settings.ha.nav":           {DE: "Home Assistant", EN: "Home Assistant"},
	"settings.ha.deviceName":    {DE: "Gerätename", EN: "Device name"},
	"settings.ha.deviceHint":    {DE: "Name des Geräts in Home Assistant.", EN: "How the device is named in Home Assistant."},
	"settings.ha.nodeId":        {DE: "Node-ID", EN: "Node ID"},
	"settings.ha.nodeHint":      {DE: "Suffix der Entities, z. B.", EN: "Entity suffix, e.g."},
	"settings.ha.topicPrefix":   {DE: "Topic-Präfix", EN: "Topic prefix"},
	"settings.ha.stateTopic":    {DE: "State", EN: "State"},
	"settings.ha.discovery":     {DE: "Discovery-Präfix", EN: "Discovery prefix"},
	"settings.ha.discoveryHint": {DE: "Standard von Home Assistant:", EN: "Home Assistant's default:"},
	"settings.ha.renameNote": {
		DE: "Wird die Node-ID oder ein Präfix geändert, werden die alten Entities in Home Assistant automatisch entfernt.",
		EN: "Changing the node id or a prefix retires the old entities in Home Assistant automatically.",
	},

	// Settings, Home Assistant long-term storage. Nothing here changes a
	// setting of this program: it says what to change in Home Assistant, and
	// hands over the exact text for it.
	"settings.recorder.title": {
		DE: "Langzeitspeicherung in Home Assistant",
		EN: "Long-term storage in Home Assistant",
	},
	"settings.recorder.nav": {DE: "Speicherung", EN: "Storage"},
	"settings.recorder.why": {
		DE: "Home Assistant schreibt jede Wertänderung in seine Datenbank. Ein PC, der hundert Messwerte im Sekundentakt meldet, füllt sie schneller, als der nächtliche Aufräumlauf sie kürzt. Unveränderte Werte kosten nichts — es kommt also darauf an, für wie viele Entities überhaupt ein Verlauf geführt wird.",
		EN: "Home Assistant writes every change of value into its database. A PC reporting a hundred readings a second fills it faster than the nightly purge empties it. Unchanged values cost nothing, so what matters is how many entities keep a history at all.",
	},
	"settings.recorder.where": {
		DE: "Der Recorder wird in <code>configuration.yaml</code> eingestellt, nicht in einer Oberfläche. Trage den folgenden Block dort ein — ist bereits ein <code>recorder:</code> vorhanden, ergänze dessen <code>exclude:</code> und <code>include:</code>, statt einen zweiten anzulegen — und starte Home Assistant neu.",
		EN: "The recorder is configured in <code>configuration.yaml</code>, not in any user interface. Add the block below — if a <code>recorder:</code> already exists, extend its <code>exclude:</code> and <code>include:</code> rather than adding a second one — and restart Home Assistant.",
	},
	"settings.recorder.what": {
		DE: "Der Block hält alle Entities dieses PCs aus dem Verlauf heraus und lässt die wenigen wieder herein, deren Verlauf über Monate etwas aussagt. Die Namen sind an deine Einstellungen angepasst: eine abgeschaltete Sensorgruppe taucht nicht auf, zwei Grafikkarten ergeben zwei Zeilen. <code>purge_keep_days</code> ist die Aufbewahrungsdauer für alles Übrige.",
		EN: "The block keeps every entity of this PC out of the history and lets back in the few whose history says something over months. The names follow your settings: a switched-off sensor group does not appear, two graphics cards give two lines. <code>purge_keep_days</code> is how long everything else is kept.",
	},
	"settings.recorder.caveat": {
		DE: "Ein Ausschluss trifft auch die Langzeitstatistik: eine ausgeschlossene Entity hat weder Verlauf noch Stunden- und Tagesmittelwerte. Beides zusammen oder gar nicht — was später einmal in einem Monatsdiagramm stehen soll, gehört in die include-Liste.",
		EN: "An exclusion also removes long-term statistics: an excluded entity keeps neither history nor hourly and daily averages. It is both or neither — anything that should appear in a monthly chart later belongs in the include list.",
	},

	// Settings, data server.
	"settings.data.title":     {DE: "Datenserver — Home Assistant und Prometheus holen ab", EN: "Data server — Home Assistant and Prometheus pull"},
	"settings.data.nav":       {DE: "Datenserver", EN: "Data server"},
	"settings.data.enabled":   {DE: "HTTP-Datenserver starten", EN: "Run the HTTP data server"},
	"settings.data.bind":      {DE: "Bind-Adresse", EN: "Bind address"},
	"settings.data.bindHint":  {DE: "<code>0.0.0.0</code> = im ganzen Netz erreichbar, <code>127.0.0.1</code> = nur lokal.", EN: "<code>0.0.0.0</code> = reachable on the network, <code>127.0.0.1</code> = local only."},
	"settings.data.port":      {DE: "Port", EN: "Port"},
	"settings.data.json":      {DE: "JSON unter <code>/api/state</code> (für den RESTful-Sensor in Home Assistant)", EN: "JSON at <code>/api/state</code> (for the Home Assistant RESTful sensor)"},
	"settings.data.prom":      {DE: "Prometheus-Exporter unter <code>/metrics</code>", EN: "Prometheus exporter at <code>/metrics</code>"},
	"settings.data.influx":    {DE: "InfluxDB Line Protocol unter <code>/influx</code> (z. B. für Telegraf)", EN: "InfluxDB line protocol at <code>/influx</code> (for Telegraf, say)"},
	"settings.data.token":     {DE: "Zugriffstoken (optional)", EN: "Access token (optional)"},
	"settings.data.tokenNone": {DE: "leer = kein Token, jeder im Netz kann lesen", EN: "blank = no token, anyone on the network can read"},
	"settings.data.tokenHint": {
		DE: "Wird geprüft als <code>Authorization: Bearer &lt;token&gt;</code> oder <code>?token=</code>. <code>/health</code> bleibt immer offen.",
		EN: "Checked as <code>Authorization: Bearer &lt;token&gt;</code> or <code>?token=</code>. <code>/health</code> is always open.",
	},
	"settings.data.clearToken": {DE: "Token löschen (Endpunkte dann ohne Authentifizierung)", EN: "Delete the token (endpoints then need no authentication)"},

	// Settings, InfluxDB push.
	"settings.influx.title":       {DE: "InfluxDB — Push", EN: "InfluxDB — push"},
	"settings.influx.nav":         {DE: "InfluxDB", EN: "InfluxDB"},
	"settings.influx.enabled":     {DE: "Messwerte aktiv an InfluxDB schreiben", EN: "Write readings to InfluxDB"},
	"settings.influx.url":         {DE: "InfluxDB-URL", EN: "InfluxDB URL"},
	"settings.influx.urlHint":     {DE: "Geschrieben wird nach <code>/api/v2/write</code>. InfluxDB 1.8 funktioniert über dieselbe API.", EN: "Writes go to <code>/api/v2/write</code>. InfluxDB 1.8 serves the same API."},
	"settings.influx.bucket":      {DE: "Bucket / Datenbank", EN: "Bucket / database"},
	"settings.influx.org":         {DE: "Organisation", EN: "Organisation"},
	"settings.influx.orgHint":     {DE: "Nur InfluxDB 2.x, bei 1.8 leer lassen.", EN: "InfluxDB 2.x only; leave blank for 1.8."},
	"settings.influx.measurement": {DE: "Measurement", EN: "Measurement"},
	"settings.influx.token":       {DE: "API-Token", EN: "API token"},
	"settings.influx.tokenNone":   {DE: "InfluxDB-2-Token, bei 1.8: benutzer:passwort", EN: "InfluxDB 2 token; for 1.8 use user:password"},
	"settings.influx.clearToken":  {DE: "Gespeicherten Token löschen", EN: "Delete the stored token"},

	// Status page, the one-off hint about the Home Assistant database.
	"status.recorder.title": {
		DE: "Home Assistant speichert jede Wertänderung",
		EN: "Home Assistant records every change of value",
	},
	"status.recorder.body": {
		DE: "Ein PC, der hundert Messwerte im Sekundentakt meldet, füllt die Datenbank schneller, als der nächtliche Aufräumlauf sie kürzt. Wie man steuert, wovon ein Verlauf geführt wird — samt fertigem Abschnitt für die configuration.yaml:",
		EN: "A PC reporting a hundred readings a second fills the database faster than the nightly purge empties it. How to control what keeps a history, with a ready-made block for configuration.yaml:",
	},
	"status.recorder.link":    {DE: "Langzeitspeicherung einrichten", EN: "Set up long-term storage"},
	"status.recorder.dismiss": {DE: "Gelesen, nicht wieder anzeigen", EN: "Read it, do not show again"},

	// Status page, the chips under the tiles. Each says what the exporter is
	// set to do right now, so each is prefixed with what it is about.
	"status.chipSet":         {DE: "Messwerte:", EN: "Measurements:"},
	"status.chipDecimals":    {DE: "Nachkommastellen:", EN: "Decimals:"},
	"status.chipDecimalsOn":  {DE: "an", EN: "on"},
	"status.chipDecimalsOff": {DE: "aus", EN: "off"},
	"status.chipInterval":    {DE: "Senden alle", EN: "Publishing every"},
	"status.chipInGame":      {DE: "im Spiel", EN: "in game"},
	"status.chipIdle":        {DE: "Leerlauf", EN: "idle"},

	// Settings, the two sensor sets.
	"settings.sensors.set":         {DE: "Messwerte", EN: "Measurements"},
	"settings.sensors.setMinimal":  {DE: "Minimal", EN: "Minimal"},
	"settings.sensors.setStandard": {DE: "Standard", EN: "Standard"},
	"settings.sensors.setExtended": {DE: "Erweitert", EN: "Extended"},
	"settings.sensors.setHint": {
		DE: "Gilt über alle Gruppen hinweg: die Schalter darunter sagen, welche Hardware gelesen wird, diese Auswahl, wie ausführlich.",
		EN: "Applies across all groups: the switches below say which hardware is read, this says how much detail of it.",
	},
	"settings.sensors.setWhat":         {DE: "Was steckt in den beiden?", EN: "What is in each?"},
	"settings.sensors.setExtendedAdds": {DE: "Erweitert ergänzt", EN: "Extended adds"},
	"settings.sensors.setStandardWhat": {
		DE: "Was man sich ansieht, wenn man wissen will, wie es dem Rechner geht: Temperatur, Auslastung, freier Platz, Durchsatz.",
		EN: "What you look at to see how the machine is doing: temperature, load, free space, throughput.",
	},
	"settings.sensors.setExtendedWhat": {
		DE: "Aufbau und Feinheiten: Taktraten, Speicherriegel, Last je Thread, Anzeigemodus, Zustand von RTSS. Nützlich beim Suchen, im Alltag selten.",
		EN: "Inventory and fine detail: clock rates, memory modules, per-thread load, display mode, the state of RTSS. Useful when hunting a problem, rarely otherwise.",
	},

	// Settings, sensor groups.
	"settings.sensors.title": {DE: "Sensorquellen", EN: "Sensor sources"},
	"settings.sensors.nav":   {DE: "Sensoren", EN: "Sensors"},
	"settings.sensors.intro": {
		DE: "Welche Hardware überhaupt gelesen wird. Fehlt die Datenquelle, entstehen dafür gar keine Entities — und sie tauchen von selbst auf, sobald die Quelle da ist. Welche einzelnen Werte davon gesendet werden, steht unter",
		EN: "Which hardware is read at all. Where the source is missing, no entities are created — and they appear by themselves once it is there. Which individual values are sent is decided under",
	},
	"settings.sensors.toMeasurements": {DE: "Messwerte", EN: "Measurements"},
	"settings.sensors.entityCount":    {DE: "Aktuell", EN: "Currently"},
	"settings.sensors.entities":       {DE: "Entities", EN: "entities"},
	"settings.sensors.gpu":            {DE: "Grafikkarte — Temperatur, Takt, VRAM, Auslastung, Lüfter, Leistung", EN: "Graphics card — temperature, clocks, VRAM, load, fan, power"},
	"settings.sensors.gpuHint": {
		DE: "Quelle ist MSI Afterburner (NVIDIA, AMD, Intel), ersatzweise NVML aus dem NVIDIA-Treiber. Ohne beides gibt es keine GPU-Werte — Windows selbst liefert weder Temperatur noch Takt.",
		EN: "The source is MSI Afterburner (NVIDIA, AMD, Intel), or failing that NVML from the NVIDIA driver. Without either there are no GPU values: Windows itself exposes neither temperature nor clocks.",
	},
	"settings.sensors.gpuLink":    {DE: "MSI Afterburner", EN: "MSI Afterburner"},
	"settings.sensors.cpu":        {DE: "Prozessor-Details — Modell, Kerne, Threads, Takt, Temperatur", EN: "Processor detail — model, cores, threads, clock, temperature"},
	"settings.sensors.cpuPerCore": {DE: "Auslastung pro Kern (erzeugt eine Entity je Thread)", EN: "Load per core (one entity per thread)"},
	"settings.sensors.ram":        {DE: "Arbeitsspeicher — belegt, frei, gesamt, Takt, Typ, Module", EN: "Memory — used, free, total, clock, type, modules"},
	"settings.sensors.ramHint": {
		DE: "Takt, Typ und Bestückung kommen aus den SMBIOS-Tabellen der Firmware; Belegung und freier Speicher direkt von Windows.",
		EN: "Clock, type and population come from the firmware's SMBIOS tables; usage and free memory come straight from Windows.",
	},
	"settings.sensors.disk":     {DE: "Laufwerke — Typ, Kapazität, Belegung, Durchsatz", EN: "Drives — type, capacity, usage, throughput"},
	"settings.sensors.details":  {DE: "Detaileinstellungen", EN: "Detailed config"},
	"settings.sensors.diskOnly": {DE: "Nur diese Laufwerke", EN: "Only these drives"},
	"settings.sensors.diskHint": {DE: "Laufwerksbuchstaben, kommagetrennt, z. B. <code>C, D</code>.", EN: "Drive letters, comma separated, e.g. <code>C, D</code>."},
	"settings.sensors.diskAll":  {DE: "leer = alle festen Laufwerke", EN: "blank = every fixed drive"},
	"settings.sensors.net":      {DE: "Netzwerk — Adapter, Link-Speed, Durchsatz, Fehler, WLAN-Signal", EN: "Network — adapter, link speed, throughput, errors, Wi-Fi signal"},
	"settings.sensors.netAll":   {DE: "Alle Adapter statt nur dem aktiven", EN: "Every adapter instead of only the active one"},
	"settings.sensors.netHint": {
		DE: "Standardmäßig wird nur der Adapter gemeldet, über den die Default-Route läuft. Sonst tauchen Hyper-V, WSL, VPN- und Capture-Adapter alle einzeln auf.",
		EN: "By default only the adapter carrying the default route is reported. Otherwise Hyper-V, WSL, VPN and capture adapters all show up separately.",
	},
	"settings.sensors.battery": {DE: "Akku — Ladestand, Netzbetrieb, Restlaufzeit, Verschleiß", EN: "Battery — charge, mains, runtime left, wear"},
	"settings.sensors.batteryHint": {
		DE: "Nur auf Geräten mit Akku. Ein Desktop meldet hier nichts, statt eine Anzeige zu erzeugen, die immer auf null steht. Verschleiß, Ladezyklen und Chemie stehen im erweiterten Messwertsatz und nur, wenn der Akku sie meldet.",
		EN: "Laptops only. A desktop reports nothing here rather than producing a gauge that sits at zero forever. Wear, charge cycles and chemistry are in the extended sensor set, and only appear when the battery reports them.",
	},
	// The crash banner. It has to say three things: that it happened, that the
	// evidence was kept, and what the reader can do with it.
	"crash.title": {
		DE: "rig-exporter ist beim letzten Start abgestürzt",
		EN: "rig-exporter crashed during the previous run",
	},
	"crash.titleUnclean": {
		DE: "rig-exporter wurde beim letzten Mal beendet, ohne sich abzumelden",
		EN: "rig-exporter ended last time without shutting down",
	},
	"crash.body": {
		DE: "Der vollständige Absturzbericht liegt neben der Konfiguration und ist aufgehoben worden.",
		EN: "The full crash record was kept, next to the configuration.",
	},
	"crash.bodyUnclean": {
		DE: "Kein Absturz mit Fehlermeldung — der Prozess wurde hart beendet, etwa über den Task-Manager, " +
			"durch einen Stromausfall oder weil die Datei überschrieben wurde. Es gibt nichts zu melden.",
		EN: "Not a crash with a stack — the process was ended hard: the task manager, a power cut, " +
			"or the executable being replaced underneath it. There is nothing to report.",
	},
	"crash.summary":  {DE: "Fehler", EN: "Fault"},
	"crash.happened": {DE: "Sitzung begann", EN: "Session started"},
	"crash.view":     {DE: "Bericht ansehen", EN: "View the record"},
	"crash.report":   {DE: "Als GitHub-Issue melden", EN: "Report it as a GitHub issue"},
	// Said before the button is pressed, not after. The page that opens shows
	// the whole text, and nothing leaves this machine until it is submitted.
	"crash.reportHint": {
		DE: "Öffnet GitHub mit ausgefülltem Bericht — Rechnername, Hardware und Windows-Version stehen darin. " +
			"Abgeschickt wird nichts, bevor du es dort selbst tust.",
		EN: "Opens GitHub with the report filled in — it names this machine, its hardware and its Windows build. " +
			"Nothing is sent until you submit it there yourself.",
	},
	"crash.dismiss": {DE: "Gelesen", EN: "Read it"},

	"settings.logs.title": {DE: "Protokolle", EN: "Logs"},
	"settings.logs.intro": {
		DE: "Was das Programm aufschreibt, ohne dass jemand einen Ordner öffnen muss. " +
			"Nichts davon verlässt diesen Rechner.",
		EN: "What the program writes down, without anybody having to open a folder. " +
			"None of it leaves this machine.",
	},
	"settings.logs.running":    {DE: "Laufendes Protokoll", EN: "Running log"},
	"settings.logs.lastLines":  {DE: "letzte %1 Zeilen", EN: "last %1 lines"},
	"settings.logs.files":      {DE: "Dateien", EN: "Files"},
	"settings.logs.crash":      {DE: "Absturz", EN: "Crash"},
	"settings.logs.open":       {DE: "öffnen", EN: "open"},
	"settings.logs.empty":      {DE: "Noch nichts aufgeschrieben.", EN: "Nothing written down yet."},
	"settings.logs.errorsOnly": {DE: "nur Fehler", EN: "errors only"},
	"settings.logs.clear":      {DE: "Aufgehobene Berichte löschen", EN: "Delete the kept records"},
	"settings.logs.clearHint": {
		DE: "Entfernt Absturzberichte und das rotierte Protokoll. Das laufende bleibt — es ist geöffnet.",
		EN: "Removes the crash reports and the rotated log. The running one stays: it is open.",
	},
	"settings.logs.noCrash": {
		DE: "Kein Absturzbericht — bisher hat sich jede Sitzung ordentlich beendet.",
		EN: "No crash report: every session so far ended on purpose.",
	},

	"settings.app.crashReport": {
		DE: "Absturzbericht als GitHub-Issue anbieten",
		EN: "Offer to report a crash as a GitHub issue",
	},
	"settings.app.crashReportHint": {
		DE: "Nur der Knopf. Der Absturz wird in jedem Fall aufgezeichnet — ob er auffällt, ist keine Einstellung.",
		EN: "The button only. A crash is recorded either way; whether it gets noticed is not a setting.",
	},

	"settings.sensors.special": {
		DE: "Spezielle Hardware — AIO-Wasserkühlung, Pumpe, Lüfter-Hub",
		EN: "Special hardware — AIO water cooling, pump, fan hub",
	},
	// The badge is not decoration. This source reads protocols nobody
	// published, against devices that mostly were never held in a hand here.
	"settings.sensors.specialAlpha": {DE: "ALPHA · ungetestet", EN: "ALPHA · untested"},
	"settings.sensors.specialHint": {
		DE: "Liest USB-Kühlungssteuerungen direkt: Flüssigkeitstemperatur, Pumpen- und Lüfterdrehzahl. " +
			"Es wird ausschließlich gelesen — an keiner Pumpe wird etwas verstellt. " +
			"Die Protokolle stammen aus LibreHardwareMonitor und sind nicht vom Hersteller dokumentiert. " +
			"Geprüft ist bisher nur die NZXT Kraken Z3; alles andere kann schweigen. " +
			"Findet sich kein Gerät, entsteht keine einzige Entity.",
		EN: "Reads USB cooling controllers directly: coolant temperature, pump and fan speed. " +
			"Reading only — nothing here changes a pump setting. " +
			"The protocols come from LibreHardwareMonitor and are not documented by their makers. " +
			"Only the NZXT Kraken Z3 has been verified against hardware; anything else may stay silent. " +
			"With no device present, not a single entity is created.",
	},
	"settings.sensors.specialLink":  {DE: "unterstützte Geräte", EN: "supported devices"},
	"settings.sensors.ping":         {DE: "Latenzmessung — Ping und Paketverlust", EN: "Latency probe — ping and packet loss"},
	"settings.sensors.pingTarget":   {DE: "Ping-Ziel", EN: "Ping target"},
	"settings.sensors.pingGateway":  {DE: "leer = Standard-Gateway", EN: "blank = default gateway"},
	"settings.sensors.pingHint":     {DE: "Hostname oder IPv4, z. B. <code>1.1.1.1</code>.", EN: "Host name or IPv4 address, e.g. <code>1.1.1.1</code>."},
	"settings.sensors.pingCount":    {DE: "Echos pro Runde", EN: "Echoes per round"},
	"settings.sensors.pingInterval": {DE: "Messintervall (ms)", EN: "Probe interval (ms)"},
	"settings.sensors.selfUsage": {
		DE: "Eigene Ressourcennutzung — CPU und Speicher von rig-exporter",
		EN: "Own resource usage — CPU and memory of rig-exporter",
	},
	"settings.sensors.selfUsageHint": {
		DE: "Zwei Werte darüber, was das Messen selbst kostet. Meist flach, deshalb standardmäßig aus.",
		EN: "Two values saying what the measuring itself costs. Mostly flat, and off by default for that reason.",
	},
	"settings.sensors.topProcs": {
		DE: "Top-Prozesse — welche Programme CPU und Speicher belegen",
		EN: "Top processes — which programs are using CPU and memory",
	},
	"settings.sensors.topProcsHint": {
		DE: "Die teuerste Option auf dieser Seite: jede Messung liest jeden laufenden Prozess. " +
			"Ergibt zwei Entities, deren Attribute sich bei jeder Messung ändern — je kürzer das " +
			"Intervall, desto schneller wächst die Datenbank von Home Assistant. " +
			"Und die Namen deiner laufenden Programme landen damit dauerhaft in deren Verlauf.",
		EN: "The most expensive option on this page: every sample reads every running process. " +
			"It produces two entities whose attributes change on every sample — the shorter the " +
			"interval, the faster the Home Assistant database grows. " +
			"And the names of the programs you run end up in its history for good.",
	},
	"settings.sensors.topProcsCount":    {DE: "Wie viele je Liste", EN: "How many per list"},
	"settings.sensors.topProcsInterval": {DE: "Messintervall (ms)", EN: "Sampling interval (ms)"},
	"settings.sensors.topProcsIntervalHint": {
		DE: "Eigener Takt, unabhängig vom Auslesen. Minimum 2000 ms.",
		EN: "Its own schedule, independent of the collection loop. Minimum 2000 ms.",
	},
	"settings.sensors.pingIntervalHint": {
		DE: "Die Messung läuft unabhängig vom Sendeintervall in einem eigenen Takt.",
		EN: "The probe runs on its own schedule, independent of the publish interval.",
	},

	// Settings, capture rates.
	"settings.capture.title": {DE: "Erfassung", EN: "Capture"},
	"settings.capture.nav":   {DE: "Erfassung", EN: "Capture"},
	"settings.capture.poll":  {DE: "Auslese-Intervall (ms)", EN: "Read interval (ms)"},
	"settings.capture.pollHint": {
		DE: "Wie oft die Hardware abgefragt wird. Bestimmt, wie flüssig Tray und diese Seite laufen; kostet etwas CPU.",
		EN: "How often the hardware is read. Sets how smoothly the tray and this page update, at a little CPU cost.",
	},
	"settings.capture.publish": {DE: "Sendeintervall im Spiel (ms)", EN: "Publish interval in game (ms)"},
	"settings.capture.publishHint": {
		DE: "Wie oft die Messwerte exportiert werden, solange ein Spiel Bilder liefert. Wird auf das Auslese-Intervall aufgerundet und darf nicht kleiner sein.",
		EN: "How often readings are exported while a game is delivering frames. Rounded up to the read interval, and never shorter than it.",
	},
	"settings.capture.publishIdle": {DE: "Sendeintervall im Leerlauf (ms)", EN: "Publish interval when idle (ms)"},
	"settings.capture.publishIdleHint": {
		DE: "Gilt, solange kein Spiel läuft. Ein Rechner im Leerlauf hat nichts zu sagen, wofür sich jede Sekunde eine Zeile in der Datenbank lohnt.",
		EN: "Applies while no game is running. An idle machine has nothing to say that is worth a database row every second.",
	},
	"settings.capture.decimals": {DE: "Berechne Nachkommastellen", EN: "Calculate decimal places"},
	"settings.capture.decimalsHint": {
		DE: "Ausgeschaltet werden alle Zahlen ganzzahlig gesendet. Ein Wert muss sich dann um eine ganze Einheit bewegen, bevor er überhaupt als geändert zählt — und was sich nicht ändert, kostet in Home Assistant keinen Speicher.",
		EN: "Switched off, every number is sent whole. A value then has to move by a full unit before it counts as changed at all — and what does not change costs no storage in Home Assistant.",
	},
	"settings.capture.idle": {DE: "Idle-Timeout (ms)", EN: "Idle timeout (ms)"},
	"settings.capture.idleHint": {
		DE: "So lange darf ein Spiel kein Bild rendern, bevor es als beendet gilt.",
		EN: "How long a game may render nothing before it counts as closed.",
	},

	// Settings, application.
	"settings.app.title":        {DE: "Anwendung", EN: "Application"},
	"settings.app.nav":          {DE: "Anwendung", EN: "Application"},
	"settings.app.language":     {DE: "Sprache", EN: "Language"},
	"settings.app.languageHint": {DE: "Gilt für Oberfläche, Tray und die Entity-Namen in Home Assistant.", EN: "Applies to this interface, the tray and the entity names in Home Assistant."},
	"settings.app.webPort":      {DE: "Port dieser Seite", EN: "Port of this page"},
	"settings.app.webPortHint":  {DE: "Änderung wirkt erst nach einem Neustart.", EN: "Takes effect after a restart."},
	"settings.app.webBindAll":   {DE: "Diese Seite im Netzwerk erreichbar machen", EN: "Make this page reachable on the network"},
	"settings.app.webBindAllHint": {
		DE: "Statt nur <code>127.0.0.1</code> lauscht der Server dann auf allen Adressen. " +
			"Auf dieser Seite stehen alle Einstellungen samt Broker-Passwort, und es gibt keine Anmeldung — " +
			"nur im eigenen, vertrauenswürdigen Netz einschalten. Wirkt erst nach einem Neustart.",
		EN: "The server then listens on every address instead of only <code>127.0.0.1</code>. " +
			"This page holds every setting including the broker password, and there is no login — " +
			"switch it on only on a network you trust. Takes effect after a restart.",
	},
	"settings.app.autostart":   {DE: "Mit Windows starten", EN: "Start with Windows"},
	"settings.app.debug":       {DE: "Debug-Logging (wirkt nach Neustart)", EN: "Debug logging (takes effect after a restart)"},
	"settings.app.updateCheck": {DE: "Auf neue Versionen prüfen", EN: "Check for new versions"},
	"settings.app.updateCheckHint": {
		DE: "Fragt alle sechs Stunden bei GitHub nach und zeigt einen Hinweis, wenn es etwas Neueres gibt. Installiert wird nur, was Sie anklicken. Abgeschaltet verlässt keine Anfrage den Rechner.",
		EN: "Asks GitHub every six hours and shows a note when something newer exists. Nothing is installed unless you click it. Switched off, no request leaves the machine.",
	},
	"settings.app.noGPU": {DE: "Keine GPU / keine Spieldaten auf diesem Rechner", EN: "No GPU / no game data on this machine"},
	"settings.app.noGPUHint": {
		DE: "Blendet auf der Statusseite FPS, Frametime, Spiel und die RTSS-Hinweise aus. Messung und Exporte bleiben unverändert.",
		EN: "Hides FPS, frame time, game and RTSS notices on the status page. Collection and exports remain unchanged.",
	},

	// Export target status.
	"export.mqtt":         {DE: "MQTT", EN: "MQTT"},
	"export.dataserver":   {DE: "Datenserver", EN: "Data server"},
	"export.influx":       {DE: "InfluxDB", EN: "InfluxDB"},
	"export.connected":    {DE: "verbunden", EN: "connected"},
	"export.disconnected": {DE: "getrennt", EN: "disconnected"},
	"export.connecting":   {DE: "verbindet…", EN: "connecting…"},
	"export.entities":     {DE: "Entities", EN: "entities"},
	"export.notStarted":   {DE: "nicht gestartet", EN: "not started"},
	"export.dropped":      {DE: "Messwerte verworfen", EN: "readings dropped"},
	"export.delivered":    {DE: "gesendet", EN: "delivered"},
	"export.lastError":    {DE: "Letzter Fehler", EN: "Last error"},
	"export.openLog":      {DE: "Log öffnen", EN: "Open the log"},

	// Tray menu.
	"tray.game":            {DE: "Spiel", EN: "Game"},
	"tray.display":         {DE: "Anzeige", EN: "Display"},
	"tray.load":            {DE: "Auslastung", EN: "Load"},
	"tray.noGame":          {DE: "kein Spiel aktiv", EN: "no game running"},
	"tray.export":          {DE: "Export", EN: "Export"},
	"tray.noExport":        {DE: "Kein Export aktiv – siehe Einstellungen", EN: "No export target — see settings"},
	"tray.rtssDownload":    {DE: "RivaTuner Statistics Server herunterladen…", EN: "Download RivaTuner Statistics Server…"},
	"tray.rtssDownloadTip": {DE: "Öffnet die RTSS-Downloadseite im Browser", EN: "Opens the RTSS download page in the browser"},
	"tray.openInterface":   {DE: "Oberfläche im Browser öffnen", EN: "Open the interface in the browser"},
	"tray.pause":           {DE: "Senden pausieren", EN: "Pause exporting"},
	"tray.pauseTip":        {DE: "Hält den Export an, ohne die Anwendung zu beenden", EN: "Stops exporting without quitting the application"},
	"tray.settings":        {DE: "Einstellungen…", EN: "Settings…"},
	"tray.settingsTip":     {DE: "Öffnet die Einstellungsseite im Browser", EN: "Opens the settings page in the browser"},
	"tray.log":             {DE: "Log öffnen", EN: "Open log"},
	"tray.autostart":       {DE: "Mit Windows starten", EN: "Start with Windows"},
	"tray.autostartTip":    {DE: "Trägt die Anwendung in den Windows-Autostart ein", EN: "Registers the application in the Windows autostart"},
	"tray.quit":            {DE: "Beenden", EN: "Quit"},
	"tray.quitTip":         {DE: "Beendet die Anwendung", EN: "Quits the application"},
	"tray.rtssNotRunning":  {DE: "läuft nicht", EN: "not running"},
	"tray.rtssDenied":      {DE: "Zugriff verweigert – als Administrator starten", EN: "access denied — run as administrator"},
	"tray.paused":          {DE: "pausiert", EN: "paused"},
	"tray.impaired":        {DE: "gestört", EN: "impaired"},

	// Message boxes.
	"dialog.alreadyRunning": {
		DE: "rig-exporter läuft bereits.\n\nDas Symbol befindet sich im Infobereich der Taskleiste.",
		EN: "rig-exporter is already running.\n\nIts icon is in the notification area of the taskbar.",
	},
	"dialog.startFailed": {
		DE: "rig-exporter konnte nicht gestartet werden:",
		EN: "rig-exporter could not be started:",
	},
	"dialog.configBroken": {
		DE: "Die Konfiguration konnte nicht gelesen werden, es gelten die Standardwerte:",
		EN: "The configuration could not be read; the defaults apply:",
	},
	"dialog.autostartFailed": {
		DE: "Autostart konnte nicht geändert werden:",
		EN: "The autostart entry could not be changed:",
	},
	"dialog.rtssDenied": {
		DE: "Auf die FPS-Daten von RivaTuner Statistics Server (RTSS) kann nicht zugegriffen werden.\n\n" +
			"RTSS läuft mit höheren Rechten als rig-exporter. Starte rig-exporter als Administrator, " +
			"oder starte RTSS ohne erhöhte Rechte.\n\n" +
			"Alle übrigen Messwerte werden weiterhin gemeldet.",
		EN: "The FPS data from RivaTuner Statistics Server (RTSS) cannot be read.\n\n" +
			"RTSS runs with higher privileges than rig-exporter. Either start rig-exporter as " +
			"administrator, or start RTSS without elevation.\n\n" +
			"Every other reading keeps being reported.",
	},
	"origins.title": {DE: "Datenquellen", EN: "Data sources"},
	"origins.hint": {
		DE: "Woher die Messwerte dieser Messung tatsächlich kamen. Nicht was möglich " +
			"wäre, sondern was dieser Rechner gerade liefert – schließt du eines der " +
			"Programme, fehlen genau dessen Werte.",
		EN: "Where this reading actually came from. Not what could be available, but " +
			"what this machine is supplying right now — close one of these programs " +
			"and exactly its values go missing.",
	},
	"origins.source": {DE: "Quelle", EN: "Source"},
	"origins.count":  {DE: "Werte", EN: "Values"},
	"origins.values": {DE: "Was sie liefert", EN: "What it supplies"},
	"origins.none": {
		DE: "Noch keine Messung.",
		EN: "No reading yet.",
	},

	"settings.sensors.pawnio": {
		DE: "PawnIO als Sensorquelle nutzen",
		EN: "Use PawnIO as a sensor source",
	},
	// Says what is true right now, not what the feature is for. The reading
	// path still needs a module, so promising temperature here would be a
	// promise the program does not keep — and a status line that overstates is
	// worse than none, because it sends people looking for a fault that is
	// really an unfinished feature.
	"settings.sensors.pawnioReady": {
		DE: "PawnIO %s erkannt, Zugriff möglich. Für Messwerte fehlt noch das " +
			"Hardwaremodul – wird beim Einschalten geladen.",
		EN: "PawnIO %s detected, access granted. Readings still need the hardware " +
			"module, which is fetched when this is switched on.",
	},
	"settings.sensors.pawnioNeedsAdmin": {
		DE: "PawnIO %s ist installiert, aber nur mit Administratorrechten erreichbar. " +
			"rig-exporter läuft ohne – als Administrator neu starten, um es zu nutzen.",
		EN: "PawnIO %s is installed but reachable only with administrator rights. " +
			"rig-exporter is running without them — restart it as administrator to use it.",
	},
	"settings.sensors.pawnioBroken": {
		DE: "PawnIO %s ist installiert, der Treiber antwortet aber nicht. " +
			"Möglicherweise gestoppt oder von Windows blockiert.",
		EN: "PawnIO %s is installed but its driver does not answer. " +
			"It may be stopped or blocked by Windows.",
	},
	"settings.sensors.pawnioMissing": {
		DE: "PawnIO ist nicht installiert. Es liefert die Prozessortemperatur auf " +
			"Rechnern ohne MSI Afterburner, installiert dafür aber einen Kerneltreiber " +
			"und verlangt, dass rig-exporter mit Administratorrechten läuft.",
		EN: "PawnIO is not installed. It supplies the processor temperature on machines " +
			"without MSI Afterburner, but installs a kernel driver and requires " +
			"rig-exporter to run as administrator.",
	},

	// PawnIO. The wording says out loud that a kernel driver is involved and
	// that administrator rights are needed afterwards. Someone agreeing to this
	// is agreeing to change how their machine is set up, and burying that in
	// "enables additional sensors" would be dishonest.
	"dialog.pawnioOffer": {
		DE: "Für die Prozessortemperatur fehlt auf diesem Rechner eine Quelle.\n\n" +
			"Windows gibt sie nicht her: AMD liefert sie über den SMU, Intel über ein " +
			"Modellregister, und beides erreicht nur Code im Systemkern.\n\n" +
			"PawnIO ist dafür der sichere Weg – ein signierter Treiber, der geprüfte " +
			"Bausteine ausführt, statt wie ältere Werkzeuge freien Registerzugriff zu " +
			"öffnen. Damit lesbar wären:\n\n" +
			"    • Prozessortemperatur, auch ohne MSI Afterburner\n" +
			"    • Leistungsaufnahme des Prozessors in Watt\n\n" +
			"Zu bedenken: PawnIO installiert einen Kerneltreiber, die Installation " +
			"verlangt Administratorrechte, und rig-exporter muss anschließend selbst mit " +
			"Administratorrechten laufen, um darauf zugreifen zu dürfen.\n\n" +
			"Alternative ohne Treiber: MSI Afterburner liefert dieselbe Temperatur und " +
			"zusätzlich die Werte der Grafikkarte.\n\n" +
			"Installationsprogramm von PawnIO jetzt herunterladen?",
		EN: "This machine has no source for the processor temperature.\n\n" +
			"Windows does not provide one: AMD reports it through the SMU and Intel " +
			"through a model-specific register, and both are reachable only from kernel " +
			"code.\n\n" +
			"PawnIO is the safe way in — a signed driver that runs verified modules, " +
			"rather than opening up raw register access the way older tools do. It would " +
			"make available:\n\n" +
			"    • Processor temperature, with no MSI Afterburner needed\n" +
			"    • Processor power draw in watts\n\n" +
			"Worth knowing: PawnIO installs a kernel driver, installing it needs " +
			"administrator rights, and rig-exporter itself must then run as " +
			"administrator to be allowed to use it.\n\n" +
			"Driver-free alternative: MSI Afterburner reports the same temperature, plus " +
			"everything about the graphics card.\n\n" +
			"Download the PawnIO installer now?",
	},
	"dialog.pawnioDownloaded": {
		DE: "Das Installationsprogramm wurde heruntergeladen:\n\n%s\n\n" +
			"Es wird jetzt geöffnet. Windows prüft dabei Signatur und Herkunft und fragt " +
			"nach Administratorrechten – rig-exporter führt es nicht selbst aus.\n\n" +
			"Nach der Installation muss rig-exporter mit Administratorrechten laufen und " +
			"PawnIO in den Einstellungen eingeschaltet werden.",
		EN: "The installer has been downloaded:\n\n%s\n\n" +
			"It will be opened now. Windows checks its signature and origin and asks for " +
			"administrator rights — rig-exporter does not run it itself.\n\n" +
			"After installing, rig-exporter has to run as administrator and PawnIO has to " +
			"be switched on in the settings.",
	},
	"dialog.pawnioFailed": {
		DE: "Das Installationsprogramm konnte nicht heruntergeladen werden.\n\n%s\n\n" +
			"Du kannst es selbst holen: %s",
		EN: "The installer could not be downloaded.\n\n%s\n\nYou can fetch it yourself: %s",
	},
	"dialog.rtssNotRunning": {
		DE: "RivaTuner Statistics Server (RTSS) ist installiert, läuft aber nicht.\n\n" +
			"Ohne laufendes RTSS können keine FPS gelesen werden. Alle übrigen Messwerte " +
			"werden trotzdem gemeldet.\n\n" +
			"Starte RTSS – oder MSI Afterburner, das RTSS mitstartet. rig-exporter verbindet " +
			"sich dann von selbst, ohne Neustart.",
		EN: "RivaTuner Statistics Server (RTSS) is installed but not running.\n\n" +
			"Without RTSS there are no FPS to read. Every other measurement is still " +
			"reported.\n\n" +
			"Start RTSS — or MSI Afterburner, which starts RTSS with it. rig-exporter will " +
			"connect on its own, with no restart.",
	},
	"dialog.rtssMissing": {
		DE: "RivaTuner Statistics Server (RTSS) wurde nicht gefunden.\n\n" +
			"Ohne RTSS können keine FPS gelesen werden. Alle übrigen Messwerte werden trotzdem gemeldet.\n\n" +
			"RTSS ist in MSI Afterburner enthalten und auch einzeln erhältlich. Nach der Installation " +
			"muss RTSS laufen; rig-exporter verbindet sich dann automatisch.\n\n" +
			"Downloadseite jetzt öffnen?",
		EN: "RivaTuner Statistics Server (RTSS) was not found.\n\n" +
			"Without RTSS no frame rate can be read. Every other reading is still reported.\n\n" +
			"RTSS ships with MSI Afterburner and is also available on its own. It has to be running; " +
			"rig-exporter then connects by itself.\n\n" +
			"Open the download page now?",
	},
}

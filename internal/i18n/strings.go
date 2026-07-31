package i18n

// catalogue holds the interface text. Keys are dotted paths that mirror where
// the string appears, so a key read in a template says where to find it.
var catalogue = map[string]Text{
	// Navigation and page chrome.
	"nav.status":   {DE: "Anzeige", EN: "Dashboard"},
	"nav.capture":  {DE: "Datengewinnung", EN: "Data capture"},
	"nav.export":   {DE: "Export & Anzeige", EN: "Export & display"},
	"nav.settings": {DE: "Einstellungen", EN: "Settings"},
	"nav.language": {DE: "Sprache", EN: "Language"},
	"page.status":  {DE: "Anzeige", EN: "Dashboard"},
	"page.capture": {DE: "Datengewinnung", EN: "Data capture"},
	"page.export":  {DE: "Export & Anzeige", EN: "Export & display"},

	"footer.config": {DE: "Konfiguration öffnen", EN: "Open configuration"},
	"footer.log":    {DE: "Log öffnen", EN: "Open log"},
	"footer.folder": {DE: "Ordner öffnen", EN: "Open folder"},

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

	"rtss.unavailable":       {DE: "RTSS nicht verfügbar", EN: "RTSS unavailable"},
	"rtss.connected":         {DE: "verbunden", EN: "connected"},
	"rtss.bannerTitle":       {DE: "RivaTuner Statistics Server läuft nicht.", EN: "RivaTuner Statistics Server is not running."},
	"rtss.bannerBody":        {DE: "Ohne RTSS gibt es keine FPS-Werte — alle übrigen Gruppen melden weiter.", EN: "Without RTSS there are no FPS values; every other group keeps reporting."},
	"rtss.download":          {DE: "RTSS herunterladen", EN: "Download RTSS"},
	"rtss.alsoInAfterburner": {DE: "(auch in MSI Afterburner enthalten)", EN: "(also part of MSI Afterburner)"},

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

	// Settings, sensor groups.
	"settings.sensors.title": {DE: "Sensorgruppen", EN: "Sensor groups"},
	"settings.sensors.nav":   {DE: "Sensoren", EN: "Sensors"},
	"settings.sensors.intro": {
		DE: "Es wird nur gemeldet, was der Rechner auch liefert. Fehlt die Datenquelle einer Gruppe, entstehen in Home Assistant gar keine Entities dafür — und sie tauchen automatisch auf, sobald die Quelle da ist.",
		EN: "Only what the machine actually supplies is reported. A group whose source is missing creates no entities at all — and they appear by themselves once the source is there.",
	},
	"settings.sensors.entityCount": {DE: "Aktuell", EN: "Currently"},
	"settings.sensors.entities":    {DE: "Entities", EN: "entities"},
	"settings.sensors.gpu":         {DE: "Grafikkarte — Temperatur, Takt, VRAM, Auslastung, Lüfter, Leistung", EN: "Graphics card — temperature, clocks, VRAM, load, fan, power"},
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
	"settings.sensors.diskOnly": {DE: "Nur diese Laufwerke", EN: "Only these drives"},
	"settings.sensors.diskHint": {DE: "Laufwerksbuchstaben, kommagetrennt, z. B. <code>C, D</code>.", EN: "Drive letters, comma separated, e.g. <code>C, D</code>."},
	"settings.sensors.diskAll":  {DE: "leer = alle festen Laufwerke", EN: "blank = every fixed drive"},
	"settings.sensors.net":      {DE: "Netzwerk — Adapter, Link-Speed, Durchsatz, Fehler, WLAN-Signal", EN: "Network — adapter, link speed, throughput, errors, Wi-Fi signal"},
	"settings.sensors.netAll":   {DE: "Alle Adapter statt nur dem aktiven", EN: "Every adapter instead of only the active one"},
	"settings.sensors.netHint": {
		DE: "Standardmäßig wird nur der Adapter gemeldet, über den die Default-Route läuft. Sonst tauchen Hyper-V, WSL, VPN- und Capture-Adapter alle einzeln auf.",
		EN: "By default only the adapter carrying the default route is reported. Otherwise Hyper-V, WSL, VPN and capture adapters all show up separately.",
	},
	"settings.sensors.ping":         {DE: "Latenzmessung — Ping und Paketverlust", EN: "Latency probe — ping and packet loss"},
	"settings.sensors.pingTarget":   {DE: "Ping-Ziel", EN: "Ping target"},
	"settings.sensors.pingGateway":  {DE: "leer = Standard-Gateway", EN: "blank = default gateway"},
	"settings.sensors.pingHint":     {DE: "Hostname oder IPv4, z. B. <code>1.1.1.1</code>.", EN: "Host name or IPv4 address, e.g. <code>1.1.1.1</code>."},
	"settings.sensors.pingCount":    {DE: "Echos pro Runde", EN: "Echoes per round"},
	"settings.sensors.pingInterval": {DE: "Messintervall (ms)", EN: "Probe interval (ms)"},
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
	"settings.capture.publish": {DE: "Sendeintervall (ms)", EN: "Publish interval (ms)"},
	"settings.capture.publishHint": {
		DE: "Wie oft die Messwerte exportiert werden. Wird auf das Auslese-Intervall aufgerundet und darf nicht kleiner sein.",
		EN: "How often readings are exported. Rounded up to the read interval, and never shorter than it.",
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
	"settings.app.autostart":    {DE: "Mit Windows starten", EN: "Start with Windows"},
	"settings.app.debug":        {DE: "Debug-Logging (wirkt nach Neustart)", EN: "Debug logging (takes effect after a restart)"},

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

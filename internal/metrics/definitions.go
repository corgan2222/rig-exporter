package metrics

import "github.com/corgan/rig-exporter/internal/i18n"

// The measurement catalogue. Adding an entry here is all it takes for a value
// to appear in MQTT discovery, the JSON endpoint, Prometheus and InfluxDB.
//
// Prometheus names and help strings stay English: they are a machine-facing
// format, read by a scraper and by whoever writes a query. Only Name follows
// the configured language, because that is the string a person sees in Home
// Assistant, in the tray and on the settings page.
//
// Entity ids come from ID plus the instance and the node id, so keep IDs short
// and lowercase — and never let them depend on the language.
//
// EntityCategory decides where Home Assistant files an entity, and the rule is:
//
//	diagnostic  Facts about the machine rather than measurements of it —
//	            model, vendor, file system, capacity, slot count, the clock a
//	            part is rated for, the limit it is held to, the operating
//	            system. Anything looked at while troubleshooting, and anything
//	            that does not move on its own. Home Assistant keeps these out
//	            of the device's main list and out of auto-generated dashboards.
//	primary     What the machine is doing right now, and what this program
//	            exists to report: frames, temperatures, load, free space,
//	            throughput, power.
//
// The awkward cases are decided by use, not by shape. Display mode is
// configuration in the abstract, but on a gaming PC a refresh rate that
// silently dropped to 60 is exactly what someone wants on a dashboard, so it
// stays primary. Idle time drives presence automations and stays primary, while
// uptime answers "how long since the last reboot" and is diagnostic — the same
// shape, a different job.
//
// The choice is recorded in testdata/catalogue.txt, so moving a measurement
// between the two shows up in review rather than surprising a user.

// Core: always collected, never optional.
var (
	FPS = Definition{
		ID: "fps", Name: i18n.Text{DE: "FPS", EN: "FPS"},
		Unit: "fps", Kind: KindGauge, Precision: 1, Group: GroupCore,
		Prom: "rig_fps", Help: "Frames per second reported by RivaTuner Statistics Server",
		StateClass: "measurement", Icon: "mdi:speedometer",
	}
	Frametime = Definition{
		ID: "frametime", Name: i18n.Text{DE: "Frametime", EN: "Frame time"},
		Unit: "ms", Kind: KindGauge, Precision: 2, Group: GroupCore,
		Prom: "rig_frametime_milliseconds", Help: "Time to render the most recent frame",
		StateClass: "measurement", Icon: "mdi:timer-outline",
	}
	Game = Definition{
		ID: "game", Name: i18n.Text{DE: "Spiel", EN: "Game"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_game_info", PromLabel: "game", Help: "Application currently being rendered",
		Icon: "mdi:gamepad-variant",
	}
	GameRunning = Definition{
		ID: "game_running", Name: i18n.Text{DE: "Spiel läuft", EN: "Game running"},
		Kind: KindBool, Group: GroupCore,
		Prom: "rig_game_running", Help: "1 while an application is rendering",
		NoEntity: true,
	}
	GamePID = Definition{
		ID: "game_pid", Name: i18n.Text{DE: "Spiel-PID", EN: "Game PID"},
		Kind: KindGauge, Group: GroupCore,
		Prom: "rig_game_pid", Help: "Process id of the rendering application",
		NoEntity: true,
	}
	Resolution = Definition{
		ID: "resolution", Name: i18n.Text{DE: "Auflösung", EN: "Resolution"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_resolution_info", PromLabel: "resolution", Help: "Active mode of the primary display",
		Icon: "mdi:monitor-screenshot",
	}
	RefreshRate = Definition{
		ID: "refresh_rate", Name: i18n.Text{DE: "Bildwiederholrate", EN: "Refresh rate"},
		Unit: "Hz", Kind: KindGauge, Group: GroupCore,
		Prom: "rig_refresh_rate_hertz", Help: "Refresh rate of the primary display",
		StateClass: "measurement", Icon: "mdi:monitor-shimmer",
	}
	DisplayWidth = Definition{
		ID: "width", Name: i18n.Text{DE: "Breite", EN: "Width"},
		Unit: "px", Kind: KindGauge, Group: GroupCore,
		Prom: "rig_display_width_pixels", Help: "Horizontal resolution of the primary display",
		NoEntity: true,
	}
	DisplayHeight = Definition{
		ID: "height", Name: i18n.Text{DE: "Höhe", EN: "Height"},
		Unit: "px", Kind: KindGauge, Group: GroupCore,
		Prom: "rig_display_height_pixels", Help: "Vertical resolution of the primary display",
		NoEntity: true,
	}
	// Collected as core readings so the dashboard tiles always have them, but
	// shown on the processor and memory panels, where they belong.
	CPULoad = Definition{
		ID: "cpu", Name: i18n.Text{DE: "Auslastung", EN: "Utilisation"},
		Unit: "%", Kind: KindGauge, Precision: 1, Group: GroupCore, Panel: GroupCPU,
		Prom: "rig_cpu_percent", Help: "System-wide CPU utilisation",
		StateClass: "measurement", Icon: "mdi:cpu-64-bit",
	}
	RAMLoad = Definition{
		ID: "ram", Name: i18n.Text{DE: "Belegung", EN: "Usage"},
		Unit: "%", Kind: KindGauge, Precision: 1, Group: GroupCore, Panel: GroupRAM,
		Prom: "rig_memory_percent", Help: "Physical memory in use",
		StateClass: "measurement", Icon: "mdi:memory",
	}
	// OSVersion is the operating system a reading was taken on, which is the
	// first thing anyone asks when a value looks wrong on someone else's PC.
	OSVersion = Definition{
		ID: "os_version", Name: i18n.Text{DE: "Windows-Version", EN: "Windows version"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_os_version_info", PromLabel: "version", Help: "Windows edition, release and build",
		EntityCategory: "diagnostic", Icon: "mdi:microsoft-windows",
	}
	// ExporterVersion is which build produced a reading. The device block in
	// Home Assistant already carries it, but nothing else does — a Prometheus
	// or InfluxDB series had no way to say which version wrote it, which is the
	// first question when two machines disagree.
	ExporterVersion = Definition{
		ID: "version", Name: i18n.Text{DE: "Version", EN: "Version"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_exporter_version_info", PromLabel: "version",
		Help:           "Version and build of rig-exporter that produced this reading",
		EntityCategory: "diagnostic", Icon: "mdi:tag-outline",
	}
	// ExporterCPU and ExporterMemory are what this program costs the machine it
	// measures. They exist because the honest answer to "does running this cost
	// me frames" is a number, not a reassurance — and because a working set that
	// only ever climbs is the shape of a leak.
	//
	// Collected only with debug logging switched on. Two entities that are
	// almost always flat are two entities nobody asked for; somebody who has
	// turned debugging on has asked.
	ExporterCPU = Definition{
		ID: "exporter_cpu", Name: i18n.Text{DE: "Exporter CPU", EN: "Exporter CPU"},
		Unit: "%", Kind: KindGauge, Precision: 1, Group: GroupCore,
		Prom: "rig_exporter_cpu_percent", Help: "CPU this exporter used, across every core together",
		StateClass: "measurement", EntityCategory: "diagnostic", Icon: "mdi:chip",
	}
	ExporterMemory = Definition{
		ID: "exporter_memory", Name: i18n.Text{DE: "Exporter Speicher", EN: "Exporter memory"},
		Unit: "MB", Kind: KindGauge, Precision: 1, Group: GroupCore,
		Prom: "rig_exporter_memory_megabytes", Help: "Working set of this exporter",
		StateClass: "measurement", EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	// Processes is a coarse but useful measure of what the machine is carrying;
	// a number that climbs and never falls is the shape of a leak.
	Processes = Definition{
		ID: "processes", Name: i18n.Text{DE: "Prozesse", EN: "Processes"},
		Kind: KindGauge, Group: GroupCore,
		Prom: "rig_processes", Help: "Number of running processes",
		StateClass: "measurement", Icon: "mdi:format-list-numbered",
	}
	// The Linux load average has no Windows equivalent — there is no run queue
	// to read — so this counts processor-equivalents busy instead: utilisation
	// times the number of logical processors, smoothed over the same three
	// windows Linux uses. A load of 4 on a 16 thread machine means four
	// threads' worth of work, exactly as it would on Linux.
	CPULoad1 = Definition{
		ID: "cpu_load_1", Name: i18n.Text{DE: "Load 1 min", EN: "Load 1 min"},
		Kind: KindGauge, Precision: 2, Group: GroupCPU,
		Prom: "rig_cpu_load1", Help: "Processor-equivalents busy, averaged over one minute",
		StateClass: "measurement", Icon: "mdi:chart-line",
	}
	CPULoad5 = Definition{
		ID: "cpu_load_5", Name: i18n.Text{DE: "Load 5 min", EN: "Load 5 min"},
		Kind: KindGauge, Precision: 2, Group: GroupCPU,
		Prom: "rig_cpu_load5", Help: "Processor-equivalents busy, averaged over five minutes",
		StateClass: "measurement", Icon: "mdi:chart-line",
	}
	CPULoad15 = Definition{
		ID: "cpu_load_15", Name: i18n.Text{DE: "Load 15 min", EN: "Load 15 min"},
		Kind: KindGauge, Precision: 2, Group: GroupCPU,
		Prom: "rig_cpu_load15", Help: "Processor-equivalents busy, averaged over fifteen minutes",
		StateClass: "measurement", Icon: "mdi:chart-line",
	}
	// The overall load stays in the core group, next to the CPU load, because
	// it is the headline number. Everything else about memory is its own
	// group, which can be switched off.
	RAMUsed = Definition{
		ID: "ram_used_mb", Name: i18n.Text{DE: "Belegt", EN: "Used"},
		Unit: "MB", Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_used_megabytes", Help: "Physical memory in use",
		StateClass: "measurement", Icon: "mdi:memory",
	}
	RAMFree = Definition{
		ID: "ram_free_mb", Name: i18n.Text{DE: "Frei", EN: "Free"},
		Unit: "MB", Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_free_megabytes", Help: "Physical memory available",
		StateClass: "measurement", Icon: "mdi:memory",
	}
	RAMFreePercent = Definition{
		ID: "ram_free_percent", Name: i18n.Text{DE: "Frei %", EN: "Free %"},
		Unit: "%", Kind: KindGauge, Precision: 1, Group: GroupRAM,
		Prom: "rig_memory_free_percent", Help: "Share of physical memory available",
		StateClass: "measurement", Icon: "mdi:chart-donut",
	}
	RAMTotal = Definition{
		ID: "ram_total_mb", Name: i18n.Text{DE: "Gesamt", EN: "Total"},
		Unit: "MB", Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_total_megabytes", Help: "Total physical memory",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	RAMClock = Definition{
		ID: "ram_clock", Name: i18n.Text{DE: "Takt", EN: "Clock"},
		Unit: "MT/s", Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_clock_megatransfers", Help: "Speed the memory modules are running at",
		EntityCategory: "diagnostic", Icon: "mdi:speedometer-medium",
	}
	RAMClockMax = Definition{
		ID: "ram_clock_max", Name: i18n.Text{DE: "Takt max.", EN: "Clock max."},
		Unit: "MT/s", Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_clock_max_megatransfers", Help: "Speed the memory modules are rated for",
		EntityCategory: "diagnostic", Icon: "mdi:speedometer",
	}
	RAMType = Definition{
		ID: "ram_type", Name: i18n.Text{DE: "Typ", EN: "Type"},
		Kind: KindText, Group: GroupRAM,
		Prom: "rig_memory_type_info", PromLabel: "type", Help: "Memory technology, e.g. DDR5",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	RAMModules = Definition{
		ID: "ram_modules", Name: i18n.Text{DE: "Module", EN: "Modules"},
		Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_modules", Help: "Number of populated memory slots",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	RAMSlots = Definition{
		ID: "ram_slots", Name: i18n.Text{DE: "Steckplätze", EN: "Slots"},
		Kind: KindGauge, Group: GroupRAM,
		Prom: "rig_memory_slots", Help: "Number of memory slots on the board",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	RAMModule = Definition{
		ID: "ram_module", Name: i18n.Text{DE: "Modul", EN: "Module"},
		Kind: KindText, Group: GroupRAM, InstanceLabel: "slot",
		Prom: "rig_memory_module_info", PromLabel: "module", Help: "One populated memory slot",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	RTSSUp = Definition{
		ID:   "rtss",
		Name: i18n.Text{DE: "RivaTuner Statistics Server", EN: "RivaTuner Statistics Server"},
		Kind: KindBool, Group: GroupCore,
		Prom: "rig_rtss_up", Help: "1 when the RTSS shared memory can be read",
		DeviceClass: "connectivity", EntityCategory: "diagnostic",
	}
	RTSSStatus = Definition{
		ID: "rtss_status", Name: i18n.Text{DE: "RTSS-Status", EN: "RTSS status"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_rtss_status_info", PromLabel: "status", Help: "Why RTSS is or is not readable",
		NoEntity: true,
	}
	RTSSVersion = Definition{
		ID: "rtss_version", Name: i18n.Text{DE: "RTSS-Version", EN: "RTSS version"},
		Kind: KindText, Group: GroupCore,
		Prom: "rig_rtss_version_info", PromLabel: "version", Help: "RTSS shared memory version",
		NoEntity: true,
	}
	Uptime = Definition{
		ID: "uptime", Name: i18n.Text{DE: "Laufzeit", EN: "Uptime"},
		Unit: "h", Kind: KindGauge, Precision: 2, Group: GroupCore,
		Prom: "rig_uptime_hours", Help: "Time since the machine booted",
		StateClass: "measurement", EntityCategory: "diagnostic", Icon: "mdi:clock-outline",
	}
	IdleTime = Definition{
		ID: "idle_seconds", Name: i18n.Text{DE: "Leerlaufzeit", EN: "Idle time"},
		Unit: "s", Kind: KindGauge, Group: GroupCore,
		Prom: "rig_input_idle_seconds", Help: "Seconds since the last keyboard or mouse input",
		StateClass: "measurement", Icon: "mdi:account-clock",
	}
)

// GPU: one instance per graphics card, addressed by index.
var (
	GPUName = Definition{
		ID: "gpu_name", Name: i18n.Text{DE: "Name", EN: "Name"},
		Kind: KindText, Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_info", PromLabel: "name", Help: "Graphics card model",
		EntityCategory: "diagnostic", Icon: "mdi:expansion-card",
	}
	// GPUVendor is who made the card. Derived from the name rather than asked
	// of a driver, because the name is the one thing every source supplies.
	GPUVendor = Definition{
		ID: "gpu_vendor", Name: i18n.Text{DE: "Hersteller", EN: "Vendor"},
		Kind: KindText, InstanceLabel: "gpu", Group: GroupGPU,
		Prom: "rig_gpu_vendor_info", PromLabel: "vendor", Help: "Graphics card manufacturer",
		EntityCategory: "diagnostic", Icon: "mdi:factory",
	}
	GPULoad = Definition{
		ID: "gpu_load", Name: i18n.Text{DE: "Auslastung", EN: "Load"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_percent", Help: "Graphics processor utilisation",
		StateClass: "measurement", Icon: "mdi:chip",
	}
	GPUTemperature = Definition{
		ID: "gpu_temperature", Name: i18n.Text{DE: "Temperatur", EN: "Temperature"},
		Unit: "°C", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_temperature_celsius", Help: "Graphics processor temperature",
		DeviceClass: "temperature", StateClass: "measurement",
	}
	GPUHotspot = Definition{
		ID: "gpu_hotspot", Name: i18n.Text{DE: "Hotspot", EN: "Hotspot"},
		Unit: "°C", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_hotspot_celsius", Help: "Hottest measured point on the graphics processor",
		DeviceClass: "temperature", StateClass: "measurement",
	}
	GPUCoreClock = Definition{
		ID: "gpu_core_clock", Name: i18n.Text{DE: "Takt", EN: "Clock"},
		Unit: "MHz", Kind: KindGauge, Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_core_clock_megahertz", Help: "Graphics processor clock",
		StateClass: "measurement", Icon: "mdi:speedometer-medium",
	}
	GPUMemoryClock = Definition{
		ID: "gpu_memory_clock", Name: i18n.Text{DE: "Speichertakt", EN: "Memory clock"},
		Unit: "MHz", Kind: KindGauge,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_memory_clock_megahertz", Help: "Graphics memory clock",
		StateClass: "measurement", Icon: "mdi:speedometer-medium",
	}
	GPUVRAMUsed = Definition{
		ID: "gpu_vram_used", Name: i18n.Text{DE: "VRAM belegt", EN: "VRAM used"},
		Unit: "MB", Kind: KindGauge,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_vram_used_megabytes", Help: "Graphics memory in use",
		StateClass: "measurement", Icon: "mdi:memory",
	}
	GPUVRAMTotal = Definition{
		ID: "gpu_vram_total", Name: i18n.Text{DE: "VRAM gesamt", EN: "VRAM total"},
		Unit: "MB", Kind: KindGauge,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_vram_total_megabytes", Help: "Total graphics memory",
		EntityCategory: "diagnostic", Icon: "mdi:memory",
	}
	GPUVRAMPercent = Definition{
		ID: "gpu_vram_percent", Name: i18n.Text{DE: "VRAM-Auslastung", EN: "VRAM usage"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_vram_percent", Help: "Graphics memory in use, relative to total",
		StateClass: "measurement", Icon: "mdi:memory",
	}
	GPUFan = Definition{
		ID: "gpu_fan", Name: i18n.Text{DE: "Lüfter", EN: "Fan"},
		Unit: "%", Kind: KindGauge, Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_fan_percent", Help: "Graphics card fan speed",
		StateClass: "measurement", Icon: "mdi:fan",
	}
	GPUFanRPM = Definition{
		ID: "gpu_fan_rpm", Name: i18n.Text{DE: "Lüfterdrehzahl", EN: "Fan speed"},
		Unit: "rpm", Kind: KindGauge,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_fan_rpm", Help: "Fastest fan on the graphics card, in revolutions per minute",
		StateClass: "measurement", Icon: "mdi:fan",
	}
	GPUPower = Definition{
		ID: "gpu_power", Name: i18n.Text{DE: "Leistung", EN: "Power"},
		Unit: "W", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_power_watts", Help: "Graphics card power draw",
		DeviceClass: "power", StateClass: "measurement",
	}
	GPUPowerLimit = Definition{
		ID: "gpu_power_limit", Name: i18n.Text{DE: "Leistungsgrenze", EN: "Power limit"},
		Unit: "W", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_power_limit_watts", Help: "Board power limit the card is allowed to draw",
		DeviceClass: "power", EntityCategory: "diagnostic",
	}
	GPUPowerPercent = Definition{
		ID: "gpu_power_percent", Name: i18n.Text{DE: "Leistungsgrenze genutzt", EN: "Power limit used"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_power_percent", Help: "Power draw relative to the board power limit",
		StateClass: "measurement", Icon: "mdi:flash",
	}
	GPUVoltage = Definition{
		ID: "gpu_voltage", Name: i18n.Text{DE: "Spannung", EN: "Voltage"},
		Unit: "mV", Kind: KindGauge,
		Group: GroupGPU, InstanceLabel: "gpu",
		Prom: "rig_gpu_voltage_millivolts", Help: "Graphics processor voltage",
		StateClass: "measurement", NoEntity: true,
	}
	GPUSource = Definition{
		ID: "gpu_source", Name: i18n.Text{DE: "Datenquelle", EN: "Data source"},
		Kind: KindText, Group: GroupGPU,
		Prom: "rig_gpu_source_info", PromLabel: "source",
		Help:           "Where the graphics telemetry came from",
		EntityCategory: "diagnostic", Icon: "mdi:import",
	}
)

// CPU: detail beyond the overall load, which lives in the core group.
var (
	CPUModel = Definition{
		ID: "cpu_model", Name: i18n.Text{DE: "Modell", EN: "Model"},
		Kind: KindText, Group: GroupCPU,
		Prom: "rig_cpu_info", PromLabel: "model", Help: "Processor model name",
		EntityCategory: "diagnostic", Icon: "mdi:chip",
	}
	CPUCoresPhysical = Definition{
		ID: "cpu_cores", Name: i18n.Text{DE: "Kerne", EN: "Cores"},
		Kind: KindGauge, Group: GroupCPU,
		Prom: "rig_cpu_cores", Help: "Number of physical processor cores",
		EntityCategory: "diagnostic", Icon: "mdi:chip",
	}
	CPUThreads = Definition{
		ID: "cpu_threads", Name: i18n.Text{DE: "Threads", EN: "Threads"},
		Kind: KindGauge, Group: GroupCPU,
		Prom: "rig_cpu_threads", Help: "Number of logical processors",
		EntityCategory: "diagnostic", Icon: "mdi:chip",
	}
	CPUClock = Definition{
		ID: "cpu_clock", Name: i18n.Text{DE: "Takt", EN: "Clock"},
		Unit: "MHz", Kind: KindGauge, Group: GroupCPU,
		Prom: "rig_cpu_clock_megahertz", Help: "Effective processor clock, averaged over all cores",
		StateClass: "measurement", Icon: "mdi:speedometer-medium",
	}
	CPUClockBase = Definition{
		ID: "cpu_clock_base", Name: i18n.Text{DE: "Basistakt", EN: "Base clock"},
		Unit: "MHz", Kind: KindGauge, Group: GroupCPU,
		Prom: "rig_cpu_clock_base_megahertz", Help: "Nominal processor frequency",
		EntityCategory: "diagnostic", Icon: "mdi:speedometer-slow",
	}
	// Windows reports the base frequency as the maximum and never mentions
	// the boost clock, so the highest reading seen is the only honest answer.
	CPUClockMax = Definition{
		ID: "cpu_clock_max", Name: i18n.Text{DE: "Takt max. (beobachtet)", EN: "Clock peak (observed)"},
		Unit: "MHz", Kind: KindGauge, Group: GroupCPU,
		Prom: "rig_cpu_clock_peak_megahertz", Help: "Highest effective clock observed since start",
		EntityCategory: "diagnostic", Icon: "mdi:speedometer",
	}
	// CPUPower is package power, which needs a kernel-backed source: the
	// energy counter it is derived from sits in a model-specific register.
	// Paired with the graphics card's power draw it gives the two numbers that
	// account for most of what a machine burns.
	// CPUVendor is who made the processor, as a value of its own rather than
	// something to be read out of the model string. An automation that wants to
	// branch on the vendor should not have to match on substrings.
	CPUVendor = Definition{
		ID: "cpu_vendor", Name: i18n.Text{DE: "Hersteller", EN: "Vendor"},
		Kind: KindText, Group: GroupCPU,
		Prom: "rig_cpu_vendor_info", PromLabel: "vendor", Help: "Processor manufacturer",
		EntityCategory: "diagnostic", Icon: "mdi:factory",
	}
	CPUPower = Definition{
		ID: "cpu_power", Name: i18n.Text{DE: "Leistung", EN: "Power"},
		Unit: "W", Kind: KindGauge, Precision: 1,
		Group: GroupCPU,
		Prom:  "rig_cpu_power_watts", Help: "Processor package power draw, when a source provides it",
		DeviceClass: "power", StateClass: "measurement", Icon: "mdi:cpu-64-bit",
	}
	CPUTemperature = Definition{
		ID: "cpu_temperature", Name: i18n.Text{DE: "Temperatur", EN: "Temperature"},
		Unit: "°C", Kind: KindGauge, Precision: 1,
		Group: GroupCPU,
		Prom:  "rig_cpu_temperature_celsius", Help: "Processor temperature, when a source provides it",
		DeviceClass: "temperature", StateClass: "measurement",
	}
	CPUCoreLoad = Definition{
		ID: "cpu_core", Name: i18n.Text{DE: "Last", EN: "Load"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupCPU, InstanceLabel: "core",
		Prom: "rig_cpu_core_percent", Help: "Utilisation of one logical processor",
		StateClass: "measurement", Icon: "mdi:cpu-64-bit",
	}
)

// Disk: one instance per volume, addressed by drive letter.
var (
	DiskLabel = Definition{
		ID: "disk_label", Name: i18n.Text{DE: "Bezeichnung", EN: "Label"},
		Kind: KindText, Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_info", PromLabel: "label", Help: "Volume label and file system",
		EntityCategory: "diagnostic", Icon: "mdi:harddisk",
	}
	DiskMedia = Definition{
		ID: "disk_media", Name: i18n.Text{DE: "Typ", EN: "Type"},
		Kind: KindText, Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_media_info", PromLabel: "media", Help: "Whether the volume is on an SSD, an HDD or NVMe",
		EntityCategory: "diagnostic", Icon: "mdi:harddisk",
	}
	DiskTotal = Definition{
		ID: "disk_total", Name: i18n.Text{DE: "Kapazität", EN: "Capacity"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_total_gigabytes", Help: "Volume capacity",
		EntityCategory: "diagnostic", Icon: "mdi:harddisk",
	}
	// DiskFilesystem used to be glued onto the label as "Windows (NTFS)". Two
	// facts in one string is one too many: nothing can filter on it, and the
	// label alone is what a person recognises the drive by.
	DiskFilesystem = Definition{
		ID: "disk_filesystem", Name: i18n.Text{DE: "Dateisystem", EN: "File system"},
		Kind: KindText, InstanceLabel: "disk", Group: GroupDisk,
		Prom: "rig_disk_filesystem_info", PromLabel: "filesystem",
		Help: "Volume file system, e.g. NTFS", EntityCategory: "diagnostic", Icon: "mdi:file-tree",
	}
	// DiskVendor is what the drive reports about itself, which is not always a
	// manufacturer: NVMe drives often leave the vendor field empty and put the
	// whole model into the product string.
	DiskVendor = Definition{
		ID: "disk_vendor", Name: i18n.Text{DE: "Hersteller", EN: "Vendor"},
		Kind: KindText, InstanceLabel: "disk", Group: GroupDisk,
		Prom: "rig_disk_vendor_info", PromLabel: "vendor",
		Help:           "Drive manufacturer as the device reports it",
		EntityCategory: "diagnostic", Icon: "mdi:factory",
	}
	DiskUsed = Definition{
		ID: "disk_used", Name: i18n.Text{DE: "Belegt", EN: "Used"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_used_gigabytes", Help: "Space in use on the volume",
		StateClass: "measurement", Icon: "mdi:harddisk",
	}
	DiskFree = Definition{
		ID: "disk_free", Name: i18n.Text{DE: "Frei", EN: "Free"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_free_gigabytes", Help: "Space available on the volume",
		StateClass: "measurement", Icon: "mdi:harddisk",
	}
	DiskUsedPercent = Definition{
		ID: "disk_used_percent", Name: i18n.Text{DE: "Belegung", EN: "Usage"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_used_percent", Help: "Share of the volume in use",
		StateClass: "measurement", Icon: "mdi:chart-donut",
	}
	DiskFreePercent = Definition{
		ID: "disk_free_percent", Name: i18n.Text{DE: "Frei %", EN: "Free %"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_free_percent", Help: "Share of the volume available",
		StateClass: "measurement", Icon: "mdi:chart-donut",
	}
	// The five overall figures sum every volume that is actually reported, so
	// an excluded drive is excluded here too. They exist because "how full is
	// this machine" is a question about the machine, and answering it from
	// four per-volume entities means doing arithmetic in a template.
	//
	// No instance: there is one of each, whatever the machine has plugged in.
	// Having none is also what makes them read as "Laufwerke Gesamtkapazität"
	// and "Drives Overall capacity" — the plural group prefix rather than the
	// singular device label, because they describe no single disk.
	DiskOverallCapacity = Definition{
		ID: "disk_overall_capacity", Name: i18n.Text{DE: "Gesamtkapazität", EN: "Overall capacity"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk,
		Prom:  "rig_disk_overall_total_gigabytes", Help: "Capacity of every reported volume together",
		EntityCategory: "diagnostic", Icon: "mdi:harddisk",
	}
	DiskOverallUsed = Definition{
		ID: "disk_overall_used", Name: i18n.Text{DE: "Gesamt belegt", EN: "Overall used"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk,
		Prom:  "rig_disk_overall_used_gigabytes", Help: "Space in use across every reported volume",
		StateClass: "measurement", Icon: "mdi:harddisk",
	}
	DiskOverallFree = Definition{
		ID: "disk_overall_free", Name: i18n.Text{DE: "Gesamt frei", EN: "Overall free"},
		Unit: "GB", Kind: KindGauge, Precision: 1,
		Group: GroupDisk,
		Prom:  "rig_disk_overall_free_gigabytes", Help: "Space available across every reported volume",
		StateClass: "measurement", Icon: "mdi:harddisk",
	}
	DiskOverallUsage = Definition{
		ID: "disk_overall_usage", Name: i18n.Text{DE: "Gesamtbelegung", EN: "Overall usage"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupDisk,
		Prom:  "rig_disk_overall_used_percent", Help: "Share in use across every reported volume",
		StateClass: "measurement", Icon: "mdi:chart-donut",
	}
	DiskOverallFreePercent = Definition{
		ID: "disk_overall_free_percent", Name: i18n.Text{DE: "Gesamt frei %", EN: "Overall free %"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupDisk,
		Prom:  "rig_disk_overall_free_percent", Help: "Share available across every reported volume",
		StateClass: "measurement", Icon: "mdi:chart-donut",
	}
	DiskRead = Definition{
		ID: "disk_read", Name: i18n.Text{DE: "Lesen", EN: "Read"},
		Unit: "MB/s", Kind: KindGauge, Precision: 2,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_read_megabytes_per_second", Help: "Read throughput since the previous collection",
		StateClass: "measurement", Icon: "mdi:download",
	}
	DiskWrite = Definition{
		ID: "disk_write", Name: i18n.Text{DE: "Schreiben", EN: "Write"},
		Unit: "MB/s", Kind: KindGauge, Precision: 2,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_write_megabytes_per_second", Help: "Write throughput since the previous collection",
		StateClass: "measurement", Icon: "mdi:upload",
	}
	DiskBusy = Definition{
		ID: "disk_busy", Name: i18n.Text{DE: "Auslastung", EN: "Busy"},
		Unit: "%", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_busy_percent", Help: "Share of the interval the volume was not idle",
		StateClass: "measurement", Icon: "mdi:gauge",
	}
	DiskTemperature = Definition{
		ID: "disk_temperature", Name: i18n.Text{DE: "Temperatur", EN: "Temperature"},
		Unit: "°C", Kind: KindGauge, Precision: 1,
		Group: GroupDisk, InstanceLabel: "disk",
		Prom: "rig_disk_temperature_celsius", Help: "Drive temperature, when the drive reports one",
		DeviceClass: "temperature", StateClass: "measurement",
	}
)

// Network: one instance per active adapter, plus a singleton latency probe.
var (
	NetType = Definition{
		ID: "net_type", Name: i18n.Text{DE: "Verbindung", EN: "Connection"},
		Kind: KindText, Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_info", PromLabel: "type", Help: "Adapter kind and address",
		EntityCategory: "diagnostic", Icon: "mdi:lan",
	}
	// "Link" rather than "Verbindungsgeschwindigkeit": the adapter name is
	// appended to it, and the compound is long enough that no panel column can
	// hold it without breaking mid-word.
	// NetIP is the adapter's IPv4 address, its own value rather than something
	// glued onto the connection type.
	NetIP = Definition{
		ID: "net_ip", Name: i18n.Text{DE: "IP-Adresse", EN: "IP address"},
		Kind: KindText, InstanceLabel: "nic", Group: GroupNet,
		Prom: "rig_net_ip_info", PromLabel: "ip", Help: "IPv4 address of the adapter",
		EntityCategory: "diagnostic", Icon: "mdi:ip-network",
	}
	NetLinkSpeed = Definition{
		ID: "net_link", Name: i18n.Text{DE: "Link", EN: "Link"},
		Unit: "Mbit/s", Kind: KindGauge,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_link_megabits_per_second", Help: "Negotiated link speed",
		EntityCategory: "diagnostic", Icon: "mdi:speedometer",
	}
	NetRx = Definition{
		ID: "net_rx", Name: i18n.Text{DE: "Empfangen", EN: "Received"},
		Unit: "Mbit/s", Kind: KindGauge, Precision: 2,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_receive_megabits_per_second", Help: "Inbound throughput since the previous collection",
		StateClass: "measurement", Icon: "mdi:download-network",
	}
	NetTx = Definition{
		ID: "net_tx", Name: i18n.Text{DE: "Gesendet", EN: "Sent"},
		Unit: "Mbit/s", Kind: KindGauge, Precision: 2,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_transmit_megabits_per_second", Help: "Outbound throughput since the previous collection",
		StateClass: "measurement", Icon: "mdi:upload-network",
	}
	NetErrors = Definition{
		ID: "net_errors", Name: i18n.Text{DE: "Fehler", EN: "Errors"},
		Unit: "1/s", Kind: KindGauge, Precision: 2,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_errors_per_second", Help: "Interface errors since the previous collection",
		StateClass: "measurement", Icon: "mdi:alert-circle-outline",
	}
	NetDiscards = Definition{
		ID: "net_discards", Name: i18n.Text{DE: "Verworfen", EN: "Discarded"},
		Unit: "1/s", Kind: KindGauge, Precision: 2,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_discards_per_second", Help: "Packets discarded since the previous collection",
		StateClass: "measurement", Icon: "mdi:package-variant-remove",
	}
	NetWifiSignal = Definition{
		ID: "net_wifi_signal", Name: i18n.Text{DE: "WLAN-Signal", EN: "Wi-Fi signal"},
		Unit: "%", Kind: KindGauge,
		Group: GroupNet, InstanceLabel: "nic",
		Prom: "rig_net_wifi_signal_percent", Help: "Wi-Fi signal quality",
		DeviceClass: "signal_strength", StateClass: "measurement", Icon: "mdi:wifi",
	}
	PingTarget = Definition{
		ID: "ping_target", Name: i18n.Text{DE: "Ping-Ziel", EN: "Ping target"},
		Kind: KindText, Group: GroupNet,
		Prom: "rig_ping_target_info", PromLabel: "target", Help: "Host the latency probe measures against",
		EntityCategory: "diagnostic", Icon: "mdi:target",
	}
	PingRTT = Definition{
		ID: "ping_rtt", Name: i18n.Text{DE: "Ping", EN: "Ping"},
		Unit: "ms", Kind: KindGauge, Precision: 1, Group: GroupNet,
		Prom: "rig_ping_rtt_milliseconds", Help: "Average ICMP round trip time",
		DeviceClass: "duration", StateClass: "measurement", Icon: "mdi:lan-connect",
	}
	PingLoss = Definition{
		ID: "ping_loss", Name: i18n.Text{DE: "Paketverlust", EN: "Packet loss"},
		Unit: "%", Kind: KindGauge, Precision: 1, Group: GroupNet,
		Prom: "rig_ping_loss_percent", Help: "Share of ICMP echoes that went unanswered",
		StateClass: "measurement", Icon: "mdi:lan-disconnect",
	}
)

// All is every definition, used to validate the catalogue in tests and to
// document what the exporter can produce.
var All = []Definition{
	FPS, Frametime, Game, GameRunning, GamePID, Resolution, RefreshRate,
	DisplayWidth, DisplayHeight, CPULoad, RAMLoad,
	RTSSUp, RTSSStatus, RTSSVersion, Uptime, IdleTime, OSVersion, Processes,
	ExporterVersion, ExporterCPU, ExporterMemory,

	RAMUsed, RAMFree, RAMTotal, RAMFreePercent,
	RAMClock, RAMClockMax, RAMType, RAMModules, RAMSlots, RAMModule,

	GPUName, GPUVendor, GPULoad, GPUTemperature, GPUHotspot, GPUCoreClock, GPUMemoryClock,
	GPUVRAMUsed, GPUVRAMTotal, GPUVRAMPercent, GPUFan, GPUFanRPM, GPUPower,
	GPUPowerLimit, GPUPowerPercent, GPUVoltage, GPUSource,

	CPUModel, CPUVendor, CPUCoresPhysical, CPUThreads, CPUClock, CPUClockBase, CPUClockMax,
	CPUTemperature, CPUPower, CPUCoreLoad, CPULoad1, CPULoad5, CPULoad15,

	DiskLabel, DiskFilesystem, DiskVendor, DiskMedia, DiskTotal, DiskUsed, DiskFree, DiskUsedPercent,
	DiskFreePercent, DiskRead, DiskWrite, DiskBusy, DiskTemperature,
	DiskOverallCapacity, DiskOverallUsed, DiskOverallFree, DiskOverallUsage, DiskOverallFreePercent,

	NetType, NetIP, NetLinkSpeed, NetRx, NetTx, NetErrors, NetDiscards, NetWifiSignal,
	PingTarget, PingRTT, PingLoss,
}

// ByGroup returns every definition in one group.
func ByGroup(group Group) []Definition {
	var out []Definition
	for _, d := range All {
		if d.Group == group {
			out = append(out, d)
		}
	}
	return out
}

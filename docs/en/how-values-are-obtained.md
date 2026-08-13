# How the values come about

**FPS and game** come from `RTSSSharedMemoryV2`. The block is mapped, read and
released afresh on every interval, so an RTSS restart is absorbed without any
intervention. The rate is `1000 × Frames / (Time1 − Time0)`, exactly what the
overlay shows.

Of all the hooked processes the foreground process wins, if RTSS knows it —
that is what you are looking at right now. Otherwise the one rendered last, so
that a game in the background keeps counting. Entries whose last frame is older
than the idle timeout drop out; that lets a game that has ended fall back to
`none` instead of freezing at the last value.

**Which game is open outlives that by fifteen seconds.** A game that has stopped
rendering has not stopped being open — alt-tab to the desktop and many of them
simply stop presenting frames — so the name and
[the identification](game-identification.md) stay put for a quarter of a minute
before falling back to `none`. What is *not* held is everything that answers a
different question: `game_running` goes false at once, the frame rate goes to
zero, and the process id is dropped entirely, because once RTSS has let go of
the entry that number may already belong to somebody else. A game that is
genuinely rendering replaces the remembered one immediately.

If RTSS has nothing, the **graphics driver** steps in, provided it counts
frames itself: AMD's ADLX does. It cannot displace RTSS, and it is not meant to
— RTSS knows game names, the process id and the time the last frame really
took, and it counts in windowed mode too. The driver counts **only in
fullscreen** and does not know what is drawing there; the game therefore stays
`none`. The frame time is derived from the rate, just as it already is for RTSS
versions without a frame time counter of their own. Where the value came from
stands as `fps_origin` in `/api/status` and in the `-probe` output — on its way
into an export it does not appear, because there every measurement looks the
same, no matter who counted it.

**FPS and frame time part company when there is nothing to measure.** The rate
still reports `0` — no frames per second is a true statement about an idle
machine, and it keeps the graph a line that drops to the floor rather than one
that breaks into segments. The frame time is left out instead: it is a duration,
and a frame that took 0 ms is the one thing it can never be. The same happens
for the single poll in which RTSS resets its own measurement window, roughly
once a second, and has counted nothing yet. The dashboard therefore leaves the
last measured frame time standing for as long as there is a frame rate at all,
and only clears the tile once the rate is gone too.

**Which game that executable is** is worked out only when
[the option](interface/data-capture.md#working-out-the-game) is on, and by three
sources in order of what they cost. Steam writes the app it launched into
`HKCU\Software\Valve\Steam\RunningAppID` and the title it keeps for it into
`…\Steam\Apps\<id>\Name`: two registry reads, no elevation, no access to the
game's process, and nothing that leaves the machine. Where Steam says nothing,
the path RTSS reported is matched against the catalogues GOG
(`HKLM\SOFTWARE\WOW6432Node\GOG.com\Games`) and Epic
(`%ProgramData%\Epic\EpicGamesLauncher\Data\Manifests\*.item`) keep on disk —
longest matching folder wins, so a game installed inside another game's
directory is reported as itself. Add-ons are dropped there: they name their base
game's folder, and "Cyberpunk 2077: Phantom Liberty" would otherwise resolve to
the expansion's own app id and show the wrong artwork.

A title found that way, and still without an app id, is worth a request to
Steam's public store search — the same one the search box on the store page
uses, without a key or an account. So is an executable that neither Steam nor a
catalogue claims at all: its file name is turned into a search term,
`Cyberpunk2077.exe` into "Cyberpunk 2077", and only what the store answers is
published — never the term itself. Programs that are never games, from browsers
to the launchers themselves, are not asked about.

That search is the one thing in this program that leaves the machine: a title or
a term goes out, an app id and the store's own spelling come back, once per term,
and the answer is kept in memory including the misses. It is never waited for
either; a title whose id has not arrived is published without one and gains it on
a later reading, because a slow store must not become a slow exporter.

Two other ways of asking Steam were measured and dropped. `steam_appid.txt` sits
in three of the installed games on the development machine, because it is a
developer file rather than something every game ships. Reading the `SteamAppId`
environment variable out of the game's process needs `ReadProcessMemory` against
a process that may be running elevated, which is both fragile and exactly the
shape a virus scanner looks for.

**Whether the machine is virtual** stands in the firmware id: vendor, product
name and BIOS vendor, which Windows files from the SMBIOS tables under
`HKLM\HARDWARE\DESCRIPTION\System\BIOS`. A guest names itself there —
`QEMU` / `Standard PC (i440FX + PIIX, 1996)`, `VMware, Inc.`, `innotek GmbH`,
`Microsoft Corporation` / `Virtual Machine`.

Deliberately **not** through the processor's hypervisor bit, although that
would be closer to hand: Windows sets it on real hardware too, as soon as
Hyper-V, WSL 2 or memory integrity is active — each of those puts the host
itself on a hypervisor. A gaming PC with VBS switched on would report itself as
a virtual machine that way, and a wrong yes is the expensive mistake here: it
sends somebody hunting a fault at the one value that is right.

The no is weaker than the yes, and `virtualized` therefore means exactly "no
known id found". A hypervisor can be set up to pass the host board's id
through; then there is nothing to see here. The name lands in `hypervisor` and
is absent entirely on real hardware, rather than appearing as empty text.

**GPU inventory** comes from DXGI 1.1. `DXGI_ADAPTER_DESC1` supplies name, PCI
id, dedicated and shared memory; Plug and Play adds the driver version and
filters out mirrored session adapters. **GPU live values** come from several
sources: Afterburner is read first, and where it answers its value holds — it
is the only source that works on every card. The vendor libraries fill the
gaps, each only for the cards it knows: on a Radeon, ADLX supplies temperature,
clock, fan and power without any extra program; on a GeForce, NVML does the
same.

Read out of Afterburner's shared memory: the sensor names are numbered per card
(`GPU1 temperature`), and the card index in the entry is not reliable —
Afterburner sets it on "RAM usage" too — so the mapping onto the DXGI instance
goes by the name.
NVML cards are paired by name in just the same way, not by index: two cards
from different vendors would otherwise be swapped.

**Resolution and Hz** come from `EnumDisplaySettingsW` for the primary monitor,
so from the display driver — independently of the process's DPI scaling.

**Windows version** is assembled, because no single source has it: the build
number from `RtlGetVersion` (`GetVersionEx` lies to programs without a manifest
and calls every Windows since 8.1 "6.2"), edition and release from the
registry. The catch is that `ProductName` there still says "Windows 10 Pro" on
Windows 11 — Microsoft never brought the value along. Whoever believes it tells
half their users the wrong operating system, so the build number decides: from
22000 on it is Windows 11. This is read once per start, because it cannot
change under a running process.

**Number of processes** through `EnumProcesses`. The function does not report
that the buffer was too small — it fills what fits, and says so only by
returning exactly as many bytes as were offered. So the buffer grows until the
answer is smaller than the room for it.

**CPU load** is the difference between two `GetSystemTimes` queries; the load
per thread comes from `NtQuerySystemInformation`.

**CPU clock** is the effective clock, not the base clock.
`CallNtPowerInformation` would be the obvious way, but on every current AMD and
most Intel it returns the nominal value unchanged — a processor running at
4.2 GHz right now reports its base clock there, and the display stands still.
The only value that moves is the performance counter `% Processor Performance`,
a percentage of the base clock that goes above a hundred when boosting. It is
read through PDH with `PdhAddEnglishCounterW`, because counter names are
translated and the same counter is called `% Prozessorleistung` on a German
Windows. Three values come out of it: **Base clock** (the nominal value from
the registry), **Clock** (the effective one) and **Clock peak (observed)**, the
highest seen since the start — Windows names the boost clock nowhere, but it
can be observed. Two queries in quick succession divide two differences close
to zero by one another, which produces outliers that would lodge permanently in
the peak; so a measuring window of at least 100 ms is required. If the counter
fails, the nominal value from `CallNtPowerInformation` remains as a fallback.

**Load** does not exist under Windows: there is no run queue to read out. So
the same thing is measured from the other side — load times the number of
logical processors, that is, how many processors' worth of work is actually
being done. Load 4 on a 16-thread machine means four threads fully busy, just
as under Linux. What this number cannot show is a queue longer than the machine
is wide — at full load it caps at the core count. It is smoothed with the same
constants as under Linux, and over the time actually elapsed: a different read
interval does not change what a one-minute average means.

**GPU power limit** is the enforced board power limit from NVML — the number
you mean when you say TDP. Together with the current draw it gives the
percentage that shows whether the limit is braking right now.

**Memory**: usage and free memory from `GlobalMemoryStatusEx`. Clock, type and
population are unknown to Windows — they stand in the SMBIOS tables, which are
reachable through `GetSystemFirmwareTable` and are parsed here directly instead
of taking the detour through WMI and COM. The clock is the configured one, so
the one the controller has settled on; with mixed population that is the one of
the slowest module. The slot designation repeats itself per channel on most
boards, so it takes channel plus designation to identify a slot.

**Drives**: usage from `GetDiskFreeSpaceEx`, type through
`IOCTL_STORAGE_QUERY_PROPERTY` (bus type and seek penalty), throughput from
`IOCTL_DISK_PERFORMANCE`. The volume is opened with no access rights at all for
this — which is exactly what allows the query without administrator rights.

**Network**: adapters from `GetAdaptersAddresses`, counters from `GetIfTable2`,
Wi-Fi signal from `wlanapi`, latency from `IcmpSendEcho`. Every counter
difference catches a reset: if a counter goes backwards, the interval is 0 and
not four billion events per second.

Error and drop counters are the ones drivers most often keep wrongly — the
Realtek adapter in the test machine reports 267 trillion received drops with
two billion added per second. Values above what the link speed physically
allows are therefore left out instead of reported: a missing entity is more
honest than one with nonsense in it. On a gateway in the LAN the ping often
sits at `0 ms`, because Windows returns the round trip only in whole
milliseconds.

## Which source supplied which value

The dashboard page has a **Data sources** panel, and `-probe` the same section:
which source, how many values, and which ones. Windows supplies the large
majority everywhere, DXGI, Afterburner, NVML and ADLX share out the graphics
values, RivaTuner supplies the frames per second, PawnIO the two values that
need kernel rights, and rig-exporter reports its own version.

The total is above the number of values, because Afterburner and NVML overlap
and the counter shows who *did* supply, not who won.

This does not come from a table; instead every measurement is stamped as it is
added. A table would describe the intended arrangement; this way what is
described is the machine in front of the user, including the case where a
program is running and still contributes nothing. Sources with several
suppliers correct the stamp themselves — that is why the graphics group
separates DXGI, Afterburner, NVML and ADLX, and why the CPU temperature appears
as an Afterburner value even though the rest of the processor source comes from
Windows.

The question the panel answers is: **what do I lose if I close this program.**

The origin reaches **no** export. It is not in JSON, Prometheus or InfluxDB,
because otherwise a dashboard could come to depend on which helper programs
happen to be running on a machine — the opposite of the promise that the same
measurement looks the same from every source.

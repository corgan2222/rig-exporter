# What gets reported

Only what the machine actually supplies is reported. Where the source of a
group is missing, no entities are created for it at all — and they appear by
themselves as soon as the source is there. Every group can be switched off on
its own.

A source that does not answer in time is skipped for that tick. The remaining
measurements go out on schedule; the group that dropped out names the reason on
the status page. The same holds when a source crashes: it is switched off
instead of taking the program with it, and the log says why.

Across all the groups lies the **scope**. The groups say which hardware is
read; the scope says how thoroughly. A slider on the **Measurements** page has
three rungs:

* **Minimal** — 16 measurements: the tiles on the dashboard page. Processor and
  graphics temperature, overall usage, throughput, battery. Deliberately
  without uptime and process count, because those change on every sample and
  fill a database with nothing.
* **Standard** — 76 measurements: what you look at when you want to know how
  the machine is doing. Temperature, load, free space, throughput, FPS.
* **Extended** (the default) — all 122: clock rates, memory modules, load per
  thread, display mode, state of RTSS, battery wear. Useful when hunting a
  problem, rarely in everyday use.

The numbers are the ceiling of each rung, not what arrives in the end. How many
measurements a machine really reports is decided by it and by you: a machine
without a battery has no battery values, one without USB cooling control no
cooling values, and every single measurement can be unticked. On a desktop
without a battery and without an AIO there are around twenty fewer than stated
here.

The slider only sets the starting position. Below it **every measurement stands
on its own to be ticked**, sorted by node, with the value it is reading right
now — so you see what you are switching on before you switch it on. What is
stored is the rung plus the deviations from it, not the list: a measurement
that a later version adds therefore comes along by itself.

Next to it stands a **rough estimate**: how many entities the selection yields
on this PC, how many database rows that is per day, and how large the database
grows from it. It does not work with guessed numbers — the share of values that
actually change is measured continuously, and the two assumed figures (300
bytes per row, 10 days of retention) stand beside it. The publish intervals are
on the same page, because they determine the row count just as much as the
selection does; the display moves along as you type. None of it needs a save
button: every change takes effect at once.

The selection has the same effect everywhere: what is not ticked is never
created in the first place and is therefore missing from MQTT, JSON,
Prometheus, InfluxDB **and** from the program's own dashboard page. A value the
dashboard shows and Home Assistant never gets would be the worse setting.

When something is unticked, the unticked entities are **removed** in Home
Assistant, not merely left unsupplied: an empty retained message goes to each
one's discovery topic, and that is exactly the delete command. The way back
announces them again. Home Assistant keeps a tombstone with area, name, icon
and labels, so any customisation survives the round trip; the recorded history
is untouched anyway.

> **Home Assistant should be running while you switch.** The deletion is a
> message, not a state — whoever is not listening misses it. And because the
> empty message also takes the old one off the broker, a Home Assistant that
> starts later finds nothing there and brings the entities back from its own
> registry as permanently *unavailable*. With it running along, they disappear
> cleanly.

The removal happens **only** on this one, explicit change. A switched-off
sensor group, or hardware that is not answering right now, leaves its entities
standing — those come back, and an entity removed in the meantime would have
lost its history for nothing.

| Group | Values | Source |
|---|---|---|
| **FPS & system** (always on) | FPS, frame time, running game, resolution, refresh rate, CPU load, RAM load, Windows version, virtual machine and hypervisor, number of processes, uptime, idle time | RTSS + Windows; FPS from the AMD driver as a fallback |
| **Graphics card** | name, vendor, driver version, dedicated and shared memory, temperature, hotspot, core and memory clock, load and how it splits across 3D, video decode, video encode and copy engine, graphics memory used, VRAM, fan (% and rpm), power, power limit and how far it is used up, voltage — per card | Windows DXGI, Plug and Play and the WDDM performance counters, MSI Afterburner, NVML (NVIDIA) and ADLX (AMD) |
| **Processor** | model, vendor, cores, threads, base, effective and highest observed clock, temperature, power, load over 1/5/15 minutes, optionally load per thread | Windows, temperature via Afterburner or PawnIO, power only via PawnIO (AMD, elevated) |
| **Memory** | used and free in MB, free in %, total, clock, maximum clock, type, populated and available slots, one entry per module | Windows + the firmware's SMBIOS |
| **Drives** | type (NVMe/SSD/HDD), label, file system, vendor, capacity, used, free, usage and free share in %, read, write, busy — per volume, plus five sums across all of them | Windows |
| **Network** | adapter, link speed, download and upload rate, total received and sent, errors, dropped packets, Wi-Fi signal, ping and packet loss | Windows + ICMP |
| **Battery** | charge, mains, charging, remaining energy, charge or discharge power, runtime left; in the extended set health, charge cycles, design and full-charge capacity, chemistry, voltage | Windows power API + the battery device |
| **Cooling** | model, liquid temperature, pump and fan speed; in the extended set the duty of pump and fan in percent | USB cooling controller (HID) |
| **Own resource usage** | CPU share and memory footprint of rig-exporter itself | Windows |
| **Top processes** | the programs with the largest CPU and memory footprint | Windows |

How many values come out of that is decided by the hardware: every graphics
card, every drive and every adapter brings its own set along. Even then the
number is not fixed — a drive whose vendor cannot be read is missing that one
value, and nobody invents it.

CPU and RAM load belong to **FPS & system**, so that they are there
independently of any switch — the tiles at the top of the page need them. They
are still shown under Processor and Memory, because that is where they get
looked for. A second sensor with the same value would be the alternative, and
two Home Assistant entities for the same number are worse than none.

## Where the graphics values come from

Windows knows every graphics card by itself: DXGI supplies model, PCI vendor,
dedicated graphics memory and the ceiling for shareable system memory; Plug and
Play adds the installed driver version. Temperature, clock and power, on the
other hand, are not part of those interfaces. So five sources mesh together:

1. **Windows DXGI** (`CreateDXGIFactory1` / `EnumAdapters1`) forms the
   inventory. It needs neither extra software nor administrator rights, and so
   it also finds an integrated Intel Iris in an ordinary laptop.
2. **The WDDM performance counters** (`GPU Engine`, `GPU Adapter Memory`)
   supply load and graphics memory in use — the same numbers the task manager
   draws on its GPU page. They come from the Windows graphics kernel, not from
   a vendor driver, need no rights and work the same on Intel, AMD and NVIDIA.
3. **MSI Afterburner** (`MAHMSharedMemory`) supplies the live values: it covers
   NVIDIA, AMD and Intel, and supplies fan, voltage and hotspot. RTSS belongs
   to it anyway, so a machine set up for the FPS overlay already has it.
4. **NVML** from the NVIDIA driver fills the gaps, above all the total VRAM
   fitted and the fan speed. Without Afterburner it is enough on its own for
   NVIDIA cards.
5. **ADLX** (`amdadlx64.dll`) is the counterpart on the AMD side and comes with
   the Adrenalin driver. It supplies temperature, hotspot, core and memory
   clock, power, fan speed, voltage and the VRAM fitted, and so is enough for
   Radeon cards without Afterburner. The bare display driver does not bring it
   along — that takes the full package.

The performance counters break the load down by **engine**: 3D, video decode,
video encode and copy engine, each as a sum across all processes. They are not
added up — three engines at 60 % each would make 180 %, and a card cannot be
busier than fully busy. The overall value is therefore the **busiest** engine,
as in the task manager.

`gpu_load` is only filled from them when neither Afterburner nor NVML supplied
it. On a machine with an NVIDIA card nothing changes, then; on a laptop with
nothing but Intel graphics there is a GPU load for the very first time.
**ADLX is deliberately not involved here:** a vendor source may only take a
value away from the counters if it measures it more accurately, and ADLX'
`GPUUsage` is a snapshot — on an RX 570 it reported 1 % while the 3D counter
stood at 39.6 %. On AMD, `gpu_load` therefore stays with the performance
counters. The graphics memory in use, by contrast, gets its **own** identifier
(`gpu_memory_used` next to `gpu_vram_used`): the one is what the graphics
kernel handed out, the other what the card itself reports. The numbers drift
apart, and a value that changes its meaning with its source is worse than two
values that each mean one thing.

Not every engine is reported. VR, OFA, Security, JPEG decode and the legacy
overlay sit permanently at zero on ordinary hardware and would be five entities
that never say anything.

The counters are read every five seconds, not on the normal sampling tick: they
supply one row per process, adapter and engine, which on an ordinary machine
comes to several hundred. The value is the average over that window, so a
longer window is not a coarser measurement but a calmer one.

On an NVIDIA card, without Afterburner only the values NVML does not know are
missing, such as hotspot and voltage. On a Radeon, ADLX now covers the same
ground; what stays open there is the power limit, which ADLX does not carry at
all, and the fan in percent — ADLX knows only the speed, and dividing the speed
by its maximum would be a different quantity under the same identifier. On
Intel, without a live source the DXGI inventory stays visible; values that
cannot be measured are left out rather than claimed as zero. Which sensors a
card has is up to the card: a Polaris Radeon answers hotspot and voltage with
`ADLX_NOT_SUPPORTED`, because it has neither sensor. NVML also reports the fan
speed (`nvmlDeviceGetFanSpeedRPM`, and the fastest fan on the card is what gets
reported) and grows new entry points with every driver generation, and
`LazyProc.Call` resolves the symbol through `mustFind` — which **panics** when
it is missing. In a binary built with `-H windowsgui` that kills the tray
without a word. So every entry point is resolved once and checked before the
first call; an old driver loses a value, not the program. The same goes for
ADLX: only two symbols are needed from there anyway, everything else runs
through function pointer tables as with DXGI, and both are checked before the
first call.

Without Afterburner and without a vendor source, then, only temperature, clock,
fan and power fall away — inventory, load and memory usage remain. Without a
kernel driver, case fans, power supply telemetry and voltages are out of reach
in principle.

The sources count through independently of one another. DXGI fixes the
instances, and Afterburner, NVML and ADLX are mapped onto them by the card name
— which is not unique, though: two identical cards are named identically. So
the mapping goes in index order and uses each instance at most once. On top of
that, the Plug and Play device list limits how often the same PCI id may occur:
otherwise a Citrix session adapter can mirror a real card in DXGI, while two
genuinely fitted identical cards are kept. On a hybrid notebook, a differing
enumeration order therefore no longer swaps Intel and NVIDIA values.

## Every drive together

Besides the per-volume values there are five sums across every reported drive:

| Field | Meaning |
|---|---|
| `disk_overall_capacity` | capacity of every drive together, in GB |
| `disk_overall_used` | of that, in use, in GB |
| `disk_overall_free` | of that, free, in GB |
| `disk_overall_usage` | share in use in % |
| `disk_overall_free_percent` | share free in % |

In Home Assistant they are called **"Drives Overall capacity"**, "Drives
Overall used" and so on — plural, while a single volume is called "Drive C:
Free". The difference is deliberate: the sums describe no particular drive.

"How full is this machine" is the question that comes before any question about
a single drive, and adding it up out of four entities in a template is work
nobody wants to do twice. That is why the sums are already part of the
**Standard** rung.

What is summed is exactly what is reported: a volume excluded through **Only
these drives** does not count, and neither does one that could not be read. The
sum is thus always the sum of what is in the list. It is calculated in bytes
and rounded only at the end — so it is more accurate than the sum of the
rounded individual values.

**What does not count as a drive in the first place:** network drives, optical
drives, removable media — and anything hanging off a USB port. The drive type
alone is not enough for that: a USB stick reports itself as removable media and
drops out right there, whereas an external USB SSD nearly always reports itself
as fixed and, without asking the storage stack, cannot be told apart from an
internal disk. So the bus is what gets asked about. A backup disk that happens
to be plugged in today would otherwise make the overall numbers jump without
anything on the machine having changed. If a drive does not answer the
question, it stays in — not knowing is no reason to leave something out.

## The active network adapter

By default only the adapter carrying the default route is reported. A machine
with Hyper-V, WSL, VPN and a capture driver otherwise has a dozen interfaces in
no time, and the one that matters is lost among them. Switchable through
**Every adapter instead of only the active one**.

If the default route falls away — cable pulled, Wi-Fi torn off — the last
active adapter keeps being reported; it does not switch over to all of them.
That is deliberate: virtual adapters do not go down with the physical card when
it fails. On a machine with six Hyper-V switches, Tailscale and ZeroTier, five
seconds without a cable would have created ninety entities — each with a
*retained* discovery message that outlives the outage. If there is no last
active adapter at all yet, the group reports nothing for that tick and names
the reason on the dashboard page.

**What can stay behind anyway.** If the machine changes its active adapter for
good — on docking, from Wi-Fi to Ethernet, or when a VPN takes over the route —
an entity is created for the new one, and the old one stays. That is intended:
an external disk unplugged for an afternoon should cost nobody their history,
and the same rule applies to adapters. Whoever wants to tidy up goes in this
order — otherwise the entity comes back at the next start:

1. delete the retained discovery on the broker (an empty message to the same
   topic, say with `mosquitto_pub -r -n -t <topic>`),
2. then remove the entity in Home Assistant.

Ping and packet loss run on a tick of their own, independent of the publish
interval: a round against an unreachable host takes seconds and must not block
the sampling loop. By default the target is the default gateway.

**Rate and amount are two different values.** Per adapter there is both:

| Field | Display | Meaning |
|---|---|---|
| `net_rx` | Download | current rate in Mbit/s |
| `net_tx` | Upload | current rate in Mbit/s |
| `net_rx_total` | Received total | amount of data in GB, since the adapter came up |
| `net_tx_total` | Sent total | amount of data in GB, since the adapter came up |

The rates are called **Download** and **Upload**, not "Received" and "Sent" —
on a device page that reads like a sum, even though Mbit/s is a speed.

The totals are the counters Windows keeps per interface, not a rate integrated
back. That is the difference between "measured" and "estimated": a Riemann sum
over a 2-second series loses all the traffic that fell between two samples.
They carry `state_class: total_increasing` — that tells Home Assistant a drop
back to 0 is a counter restart and not negative traffic. Windows does reset
these counters when an adapter is reconfigured.

GB here means 2³⁰ bytes, as everywhere else in the catalogue and as in Windows
Explorer. That is why the two deliberately carry **no** `device_class`: Home
Assistant would read `data_size` as 10⁹ and start from the wrong basis when
converting.

## Special hardware — AIO cooling

Water coolers, pumps and fan hubs hang off the machine as a USB device and
announce themselves as HID. They send their state of their own accord, roughly
once a second — it is enough to listen. No vendor program, no driver, no
administrator rights, and the vendor's software can keep running alongside.

**It is read only.** Nothing is changed on any pump curve or fan profile; the
program does not write a single byte to such a device.

The source is called **Special hardware** in the interface and is marked
`ALPHA · untested`. That is meant literally: the protocols have not been
published by any vendor, they come from
[LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/tree/master/LibreHardwareMonitorLib/Hardware/Controller),
and **one** family has been verified against real hardware so far.

| Device | USB id | State |
|---|---|---|
| NZXT Kraken Z3 | `1E71:3008` | verified against real hardware |
| NZXT Kraken X3 | `1E71:2007`, `1E71:2014` | same protocol, unverified |
| NZXT Kraken 2023 / Elite | `1E71:300C`, `1E71:300E` | same protocol, unverified |
| NZXT Kraken Elite V2 | `1E71:3012` | same protocol, unverified |

If no matching device is found, not a single entity is created — as with every
other group. On the dashboard page and in the measurement selection the box
then does not turn up at all.

## When it crashes

A program built with `-H windowsgui` starts without a console flashing up — and
for that it has **no stderr**. Which is exactly where Go writes a panic
together with its stack trace. Without a countermeasure, then, the very thing
that explains the fault disappears: the process is gone, the log says nothing,
and the Windows event log says nothing either, because the Go runtime handles
the fault itself.

So rig-exporter gives itself a stderr back at startup and points it at
`crash.log` next to the configuration. What the runtime writes when it comes to
it lands on disk instead of into nothing — including the panic of a goroutine
that no `recover` could ever catch.

A session that ends properly empties the file again — and that means every
planned shutdown, not just the one through the tray. An update handing over to
its helper, and a start aborted with an error dialog, therefore leave no crash
record behind. If there is something in it at the next start, the last run was
not one of those:

| Content | Meaning |
|---|---|
| empty | ended cleanly |
| only the header line | ended hard — task manager, power cut, overwritten EXE |
| panic with stack trace | a fault in the program |

The record is set aside, the log gets an error line, and a box appears at the
very top of the dashboard page. It is kept under the name

```
rig-exporter_<machine>_crashreport_<date>_<time>.log
```

— and the last ten stay put. The machine name is in it because such files
travel: attached to an issue, dragged into a chat, sent by mail. A folder full
of `crash-<date>.log` from three PCs cannot be told apart afterwards.

Four actions hang off the box, and three of them are also on every file under
[*Export & display → Logs*](interface/export-and-display.md#logs): view, download
and — there only for a crash record — the GitHub button. The ✓ exists only on
the box.

| Symbol | What it does |
|---|---|
| 👁 | view the record in the browser |
| ⤓ | download it as a file |
| GitHub | open GitHub with the bug report filled in |
| ✓ | take the notice away (the file stays) |

**Opened, not submitted.** What goes into the report is a fixed list: version,
Windows build, interface language, processor, graphics card, which GPU sources
answered, whether it ran elevated, and the record itself. The configuration is
deliberately not read, because the broker password and three tokens are in
there.

Before it is filed away the record is washed — the **file** too, not just the
link. Replaced by `<removed>` are: your own user path, credentials in URLs
(`tcp://name:passwort@broker`), and every key whose name ends in `password`,
`passwd`, `token`, `secret`, `apikey` or `api_key` — so `mqtt_password` and
`influx_token` as well, in the form `name=value` as in JSON. Plus a
`Bearer <value>` in a header field.

**Read the record anyway before you attach it.** The wash is a fallback for the
case that some day a log line is added that thinks of none of this — not a
guarantee. Nothing is submitted before you do it yourself on the GitHub page;
the button can also be switched off entirely under
[*Export & display → Application*](interface/export-and-display.md#application).

This is offered for a session that simply vanished as well, not only for a
panic with a stack trace. A program that is gone without a word is exactly the
fault this was built for, and the report carries the build, the machine, the
answering sources and the last 200 log lines. Whether somebody ended the task
on purpose is something the program cannot know — the box asks, and whoever
reads it knows.

The recording happens either way. Whether a crash gets noticed is not a
setting.

## The battery

The battery group is the only one that stays empty on most machines, and that
is intended: a desktop produces **not a single entity** here. A display that
permanently claims "0 %" would be the worse answer than none at all. On the
dashboard page the box is missing there too, and on the **Measurements** page
the battery rows are missing — a missing graphics card is worth a message, a
missing battery in a tower is not. A battery that is there and does not answer
is reported, by contrast. The selection itself stays untouched in all this: the
configuration of a machine without a battery keeps the battery measurements, so
that the same file arrives complete on a laptop.

Two sources feed it, and they answer different questions. The Windows power
interface says how the battery is doing **right now** — how full, on mains or
not, charging or discharging, how long it will last. The battery device itself,
through SetupAPI and the battery IOCTLs, says **what** the battery is: how
large it was when new, how many charge cycles it has behind it, what it is made
of. Only the second way can say anything about wear, and only the first is
cheap enough to go every few seconds — the device values are therefore fetched
again every five minutes.

Neither of the two ways needs administrator rights, WMI or a foreign driver.

**Health** (`battery_health`) is today's full-charge capacity divided by the
design capacity — put the right way round, so that a fresh battery sits at 100
and wanders down; that draws better in Home Assistant than the reciprocal.
Design and full-charge capacity stand beside it, so the number can be checked
rather than believed.

**Power** (`battery_power`) is signed: positive while the battery takes charge
in, negative while it gives it out. One series then shows the whole picture
instead of two, of which both are never interesting at once.

What the battery does not give up is left out, and that is more than one might
think. Many controllers count **no charge cycles** and report a permanent 0 —
no entity comes of that, because "0 cycles" reads like a factory-fresh battery.
Runtime left exists only while discharging. If a controller reports its
capacities in units of its own instead of in milliwatt-hours, all the Wh values
fall away; charge, health and cycles remain, because they do not depend on
them.

The battery's serial and manufacturer number would be available the same way
and are deliberately **not** reported: identifying, without saying anything
about the state of the machine.

A device with two batteries still reports one: Windows merges the live values
anyway, the capacities are added, and the cycle count is the higher of the two
— the more tired cell is the answer that matters.

---

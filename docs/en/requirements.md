# Requirements

**The only real requirement is Windows 10 or 11 (64 bit).**

Everything else is voluntary. Download, double-click, done: no installation, no
runtime, no library, no administrator account. In that state the program
already measures most of its catalogue — processor, memory, drives, network,
battery and the inventory of the graphics cards are read by Windows itself.

Additional programs each unlock one clearly bounded area. What is missing is
left out rather than invented as a zero, and it appears by itself as soon as
the source is there — a restart is never needed for that.

## What each program adds

| Source | Needed? | What only it makes available |
|---|---|---|
| **Windows alone** | already there | Processor (model, cores, clock, load), memory, drives, network, battery, Windows version, latency, top processes, own resource usage — plus the GPU inventory via DXGI and the GPU load via the WDDM performance counters |
| **Graphics driver** — NVML on NVIDIA, ADLX on AMD | already there | GPU temperature, core and memory clock, total VRAM, fan speed, power. On AMD, ADLX also counts the frames per second without RTSS, and the frame time from it — but only in fullscreen and without a game name. Comes with the driver, nothing to install |
| [**RivaTuner Statistics Server**](https://www.guru3d.com/download/rtss-rivatuner-statistics-server-download/) | no | **Frames per second, frame time and the name of the running game.** The name comes from nowhere else; the rate is otherwise counted only by the AMD driver, and only in fullscreen |
| [**MSI Afterburner**](https://www.msi.com/Landing/afterburner/graphics-cards) | no | **CPU temperature** on every platform; plus the live GPU values where the driver does not give them up (Intel, older cards). Contains RTSS already |
| [**PawnIO**](https://pawnio.eu/) | no | **CPU power in watts** — available no other way — and the CPU temperature without Afterburner. AMD only for now, and only with administrator rights |

The rows are meant to add up, but they do not depend on each other: RTSS
without Afterburner makes just as much sense as Afterburner without RTSS.
Anyone who only wants to know how warm and how full the machine is needs
neither of them.

To **build** from source: Go 1.26 or newer (`go.mod` demands 1.26.5, the
dependencies on their own already 1.25). No CGO, no C compiler.

## Privileges and running programs

Nothing about the program needs administrator rights — with the single
exception of PawnIO, and that one is switched on deliberately by hand.

There is a trap in it: if RTSS or Afterburner runs **elevated** and this
program does not, Windows denies access to their shared memory. FPS or the
temperatures are then missing although both programs are visibly running.
Either both elevated or neither.

If RTSS is missing, a note with a download link appears **on the first start** —
and not again after that. Every other group keeps running without RTSS, and the
state is shown in the tray and on the Dashboard page. A machine without RTSS is
a perfectly usable machine for everything else, and nothing here is waiting on a
dialog.

When RTSS is closed, its shared memory does **not** disappear: RTSSHooks stays
loaded inside every hooked application, the section outlives the process, and
on the way out RTSS overwrites its signature with `0xDEAD` — per its own SDK,
"marked for deallocation". That is reported as "not running", not as an error.
If RTSS starts later, the program connects by itself: the mapping is opened
afresh on every read, so a restart is never needed.

## PawnIO

Download: **[pawnio.eu](https://pawnio.eu/)** — the same address the "Data
capture" page links next to the checkbox. The offer on the first start, by
contrast, fetches straight from the GitHub release of PawnIO.Setup.

PawnIO is a signed kernel driver that runs verified bytecode — the safe
successor to WinRing0, which sits on Microsoft's driver blocklist for its
unrestricted register access. It makes processor temperature and power readable
without Afterburner, and the power readable at all.

**AMD only so far.** The module used is `AMDFamily17.bin`, which covers
families 17h through 1Ah; on anything else the source reports "only AMD
processors are supported so far" and delivers nothing. On an Intel machine the
installation is therefore not worth it at present — there, Afterburner is the
way to the CPU temperature, and a package power does not exist at all.

Detecting it takes no privilege whatsoever: `PawnIOLib.dll` loads and reports
its version from an ordinary process too. **Using** it that way is not
possible. PawnIO's device carries a protected ACL,
`D:P(A;;GA;;;SY)(A;;GA;;;BA)` — LocalSystem and administrators only. Measured:
from a non-elevated process, `pawnio_open` returns `0x80070005`,
E_ACCESSDENIED.

That is where the split comes from. Detection always runs and distinguishes
four states, because they lead to four different pieces of advice: not
installed, installed but out of reach without administrator rights, driver does
not answer, usable. Telling somebody to "install it" who has had it for ages is
worse than saying nothing.

It is switched on only deliberately, in the settings. Off as long as nobody
agrees: switching it on means letting rig-exporter run with administrator
rights, and that is a decision about the machine, not a setting.

On the first start, and only then if PawnIO is missing, an offer appears. It
says outright that a kernel driver gets installed and that administrator rights
are needed afterwards, and it names MSI Afterburner as the driver-free
alternative. Whoever agrees gets the installer downloaded — and it is checked
that the redirect chain really ends on a GitHub release host over HTTPS. It is
**not** run by this program: it goes to Windows via `ShellExecute`, so that the
signature check, SmartScreen and the rights prompt happen where they can be
seen.

PawnIO is not shipped along. It is under GPL-2.0, its modules under LGPL-2.1;
the user installs it, this program only looks for it.

The modules come from a **fixed** release of PawnIO.Modules, not from whichever
is newest. They are fetched once and kept under
`%APPDATA%\rig-exporter\modules`. A new module version therefore arrives with a
new rig-exporter version and not on its own — which, for signed code that ends
up in a kernel driver, is the right direction.

The fetching does not hold up the measuring. It runs alongside the measuring
loop, and as long as nothing is loaded this source supplies nothing — every
other value keeps coming unchanged. If it fails, because there is no network
just then say, it is tried again later: first after a minute, then at growing
intervals up to an hour. A laptop coming back onto Wi-Fi does not need the
program restarted for that.

**Otherwise the CPU temperature only comes with Afterburner.** That is no
convenience: Ryzen reports Tctl through the SMU, Intel through an MSR, and both
sit in ring 0. No program without a kernel driver reaches them — which is why
Afterburner brings one along. The driver-free routes have been measured and are
all dead: ACPI thermal zones (zero instances via PDH, SetupDi and WMI alike),
`Win32_TemperatureProbe` (needs an SMBIOS structure that consumer boards do not
write) and `CallNtPowerInformation` (has no temperature field).

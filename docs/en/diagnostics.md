# Diagnostics

```bash
.\rig-exporter.exe -probe
```

Takes two readings four seconds apart and writes everything out, grouped by
sensor group, followed by JSON, the Prometheus exposition and line protocol. The
quickest way to see which sources are working and what would arrive in Home
Assistant.

On a laptop without Afterburner or an NVIDIA driver, the **Graphics card**
section in the Extended scope has to contain at least Name, Vendor, Driver
version, Dedicated GPU memory and Shared GPU memory, plus `Windows DXGI` as the
Data source. Load, Temperature and Clock are missing in that case on purpose, as
long as no live source really measures them.

The output **always additionally** lands in `%APPDATA%\rig-exporter\probe.txt`,
and there is a reason for that. The program is linked as a GUI application so
that no console window flashes up at startup; it therefore has no console of its
own and borrows the one of the calling terminal. How the output is caught after
that depends on the shell: PowerShell's `>` silently produces an empty file for a
GUI program, while `| Out-File`, `cmd /c >` and the direct call work. A diagnosis
whose result depends on which redirection somebody typed is not one — hence there
is always a file, and its path stands at the end of the output.

| Symptom | Cause |
|---|---|
| RTSS `not_running` | RTSS has not been started. |
| RTSS `access_denied` | RTSS runs elevated, rig-exporter does not. Bring one of the two into line. |
| FPS stays at 0, game `none` | RTSS is not hooking the application. Check "Application detection level" in the RTSS profile. On a Radeon the driver steps in without RTSS, but only in fullscreen — in windowed mode it stays at 0. |
| No GPU group | Is the GPU group switched on in the settings? DXGI and the Windows device list found no physical adapter. An unreachable Afterburner only affects the live values. |
| No CPU temperature | Comes from Afterburner, or from PawnIO — that one only on AMD and only elevated. |
| No CPU power | Available exclusively through PawnIO: switched on, AMD, elevated. |
| No throughput values | Only present from the second reading on; they are a difference. |
| Entities missing in HA | Is the MQTT integration active? Is the discovery prefix identical? Check the log. |

## Logs in the browser

Under *Export & display*, right at the bottom, sits the
[**Logs**](interface/export-and-display.md#logs) box. It shows the last 200 lines
of the running log, at the same levels they were written at:

| Level | Colour |
|---|---|
| DEBUG | purple |
| INFO | white |
| WARN | yellow |
| ERROR | orange |
| Crash, panic, hard termination | red |

![The Logs box with the running log and the files below it](images/screenshots/en/export-logs.png)

*errors only* hides the quiet levels without loading anything. Below them stand
the files: the running log, the rotated one before it and every kept crash
record, each with View in the browser, Download and — after a crash — the GitHub
button. None of it leaves this machine as long as nobody presses one of them.

Only what stands in this list is ever served. The `config.json` sits in the same
folder and is not handed out this way, not even under a cleverly written name.

## What the exporter itself costs

A sensor group of its own, below Latency probe, **off** by default:

| Field | Meaning |
|---|---|
| `exporter_cpu` | this process's share of the CPU in %, across all cores together |
| `exporter_memory` | this process's working set in MB |

They answer "is measuring costing me frames" and "does the memory use grow over
days" with a number instead of an assurance. The percentage takes the same
denominator as the task manager: 100 % would mean every core busy, not one. The
first reading after the start reports 0 %, because a difference needs two
readings.

Off as long as nobody asks — two values that are almost always flat are two
entities nobody asked for, and a percentage showing 0.0 all day long looks like a
broken sensor rather than a frugal program.

The values appear immediately after saving, without a restart. When switched off,
the two entities disappear in Home Assistant as well; for that HA has to be
running at that moment.

## Top processes

The most expensive option in the program, a sensor group of its own, **off** by
default. It answers the one question none of the other values can answer: the
processor was at 80 %, but *who* was that.

| Field | Meaning |
|---|---|
| `top_cpu` | the N programs with the largest share of the CPU, in % of the whole machine |
| `top_memory` | the N programs with the most private memory, in % of RAM |

Grouping is by program, not by process: a browser is one entry, not the dozens of
processes it has spread across. Memory counts **private bytes** instead of the
working set, because working sets cannot be added up — every one of those
processes maps the same DLLs, and whoever adds them together credits the browser
with gigabytes that exist only once. The accounting buckets `Idle`, `System`,
`Memory Compression`, `Registry` and `vmmem` drop out; otherwise `Idle` would top
the CPU list by a wide margin on a quiet machine.

The CPU share refers to the whole machine, as in the task manager: a program that
fully loads exactly one thread sits at a hundred divided by the number of threads
— not at 100. That is the only denominator under which two machines with
different core counts can be compared at all.

**Why this is expensive:** every sample reads every running process, in a single
call. On an ordinary Windows that is several hundred, and the call takes
milliseconds rather than microseconds — it costs processing time and blocks while
it runs. That is why the sampling has a **rate of its own** (10 s by default,
2000 ms minimum) and does not hang off the read interval: at one second it would
run constantly and would stand in the measuring loop every time.

The second price is paid in the Home Assistant database. The attributes change
with every sample, so two rows arise per sample — at 10 seconds over 17,000 a
day, at 30 seconds a third of that. And the names of the running programs are
thereby permanently in the history; whoever shares the machine should know that.

### The shape: one sensor with a table instead of five entities

Each of the two lists is **one** entity. Its state is the name of the
front-runner, the full list hangs off it as an attribute:

```yaml
sensor.re_corganpc2_top_cpu
  state: firefox.exe
  attributes:
    top: firefox.exe
    apps:
      - {name: firefox.exe, value: 41.2}
      - {name: cs2.exe,     value: 12.0}
    rank1: 41.2
    rank1_name: firefox.exe
    rank2: 12.0
    rank2_name: cs2.exe
```

The same five entries three times over, because three different readers need
three different shapes: `top` for the state of the entity, `apps` for showing a
table, and `rank1`…`rank5` flat — because **only a number can be plotted**, a
list of objects cannot. If there are fewer programs than N, the trailing ranks
are missing instead of turning up as zero: a zero would mean "a program that
consumes nothing", and that is not what happened.

Five entities per list would have been the alternative — `top_cpu_1` through
`top_cpu_5`. What speaks against it is that the program behind place 2 changes
every few minutes: a time series called "place 2" records something different at
every change and is worthless as a history. Five rows stay five rows when they
sit together in one attribute.

The price of that choice: **no long-term statistics.** Home Assistant builds no
`statistics` from attributes, and none from a text state either. The history
therefore lives only as long as `purge_keep_days` (10 days by default). The two
entities are for that reason in the `include` list of the generated
`recorder:` block — excluding them would leave nothing at all to plot.

### Decimal places: always here, and two for the CPU

The two rankings do **not** hang off the *Calculate decimal places* switch. The
switch exists so that values change less often — what does not change costs no
row in the Home Assistant database. A table gains nothing there: its attributes
are rewritten on every sample anyway. What the rounding would cost, on the other
hand, is exactly what the list is there for.

The CPU share therefore has **two** decimal places, memory one. A share of the
whole machine is below one percent for most background programs, and the more
cores a machine has, the smaller the numbers get. With one place the trailing
positions then all fall onto the same value — the chart is bars of equal height,
although the programs differ by a multiple. The second place separates them
again.

A memory share never gets that small; a second place there would be nothing but
noise.

## Known limitations

Two cases in which a value is not missing but wrong — that has to be said before
somebody builds an automation on it:

* **Hybrid CPUs** (Intel 12th generation and newer, P and E cores): the reported
  clock is **systematically too high** there. The performance counter averages
  ratios against nominal frequencies of their own, whereas here a single base
  clock is multiplied. All the other CPU values are correct.
* **More than 64 logical processors:** core count and overall load are correct;
  only the optional per-core list covers one processor group, so at most 64 of
  them.

## Updates

Checked directly at startup and every **six hours** after that. Can be switched
off under [**Export & display → Application**](interface/export-and-display.md#application)
**→ Check for new versions**; on from the factory.
Switched off, no request leaves the machine, and nothing is offered either —
neither in Home Assistant nor on the Dashboard page.

If there is something newer, these are two paths to the same thing:

* **On the Dashboard page** a box appears with the new version number, the
  installed one beside it, a link to the release notes and an **Update now**
  button.
* **In Home Assistant** MQTT announces a native **update entity**, with the same
  details and a short excerpt from the changelog. The excerpt keeps the headings
  and the list items of the release notes, because Home Assistant renders it as
  markdown, and is limited to the 255 characters Home Assistant provides for.
  What does not fit is left out whole instead of being cut mid-sentence, and a
  closing `- …` says that something was left out. The link therefore always
  leads to the unabridged changelog — over MQTT there is no dialog for the full
  release notes, only integrations written in Python can offer one.

The picture on the entity is the rig-exporter mark, fetched from the handbook.
That address is the same seen from every machine, so it also survives the
interface moving to a different port. A browser that cannot reach the internet
shows a speedometer instead.

The exporter installs nothing unattended. Only the click — here as there —
triggers the download. While it runs, the button shows the process going on.
Afterwards rig-exporter shuts down in an orderly way, swaps the EXE and starts
again in the background. The new instance reports back the version it is actually
running, and only then does the swap count as successful; if it does not report
in, the old build is fetched back.

Only the EXE that is **actually running** is replaced. A call that wanted to swap
a different file is refused.

Official update artefacts are signed. Before the swap the exporter checks the
signature of the published checksums and then the SHA-256 checksum of the Windows
archive. If one of these checks fails, the existing EXE is not replaced. The
release workflow, too, aborts if the signing key is missing or its signature does
not match the built-in public certificate.

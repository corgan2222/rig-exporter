# AGENTS.md

Instructions for coding agents working in this repository.
[`CONTRIBUTING.md`](CONTRIBUTING.md) applies in full and is not repeated here.
[`AI_POLICY.md`](AI_POLICY.md) says what a pull request made with an agent has
to meet.

## Build and check

```powershell
.\build.ps1 -Check          # gofmt, go vet, staticcheck, all tests, then the build
.\build.ps1 -Check -Race    # the same with the race detector; needs cgo and mingw-w64
```

Go 1.26.5 or newer, windows/amd64. The shipped build uses no cgo. A change is
ready when `-Check` is green.

Never judge behaviour from a plain `go build`. Without `-H windowsgui` the
result is a console binary, and `-probe` prints where the shipped build stays
silent.

## Testing from the command line

Every feature gets a command-line entry that takes the same code paths as the
interface. Check behaviour by command. CI has no window to click.

| Flag | Effect |
|---|---|
| `-version` | prints the version and exits |
| `-probe` | prints one reading in every export format and exits |
| `-config <path>` | reads another `config.json`, so a test leaves the real one alone |
| `-demomode` | adds a simulated game to the tray menu |
| `-background` | starts without opening the interface |

`-probe` also writes `%APPDATA%\rig-exporter\probe.txt`. PowerShell's `>`
leaves an empty file for a GUI binary; use `| Out-File` or `cmd /c "... > x"`.

Only one tray instance runs per user. A second start shows a dialog and exits
with code 0, even with its own `-config`. From a background shell that looks
like a clean start. `-version` and `-probe` run before that check and work next
to a running instance.

## Where things live

| Path | Content |
|---|---|
| `internal/metrics` | the measurement catalogue, sets, rendering into every format |
| `internal/collector` | builds a snapshot from the sources |
| `internal/hardware/*` | the data sources: GPU, CPU, RAM, disk, network, PawnIO |
| `internal/hamqtt` | MQTT publisher and Home Assistant discovery |
| `internal/export/*` | data server, InfluxDB push |
| `internal/webui` | interface, templates, `/api/status` |
| `internal/app` | measurement loop, configuration changes, runner |
| `internal/winapi` | the Win32 calls `x/sys/windows` does not cover |
| `docs/en`, `docs/de` | the manual, one file per chapter, both languages |

## Rules beyond CONTRIBUTING.md

**The cheapest source wins.** When several sources can deliver the same value,
take the one that costs the least. The expensive source runs last in `Collect`
and only fills what the cheap one left empty. Two exceptions need discussion
first: the expensive source measures more precisely, or the two values only
look alike. Then they are two measurements with identifiers of their own.

**Win32 procs go through `internal/winapi`.** `windows.LazyProc.Call` panics on
a missing symbol. Use `winapi.Call` when the function returns a value,
`winapi.CallStatus` when it returns a Win32 error code, and
`winapi.CallNTStatus` for an NTSTATUS. The wrong helper reads a missing symbol
as success.

**No licence header in source files.** `LICENSE` covers the repository.
`THIRD-PARTY-NOTICES.md` is regenerated on every change to `go.mod` and never
edited by hand.

## Documentation

The manual is the MkDocs site under `docs/`. `README.md` is only the landing
page.

- A pull request that changes `docs/en/x.md` also changes `docs/de/x.md`, and
  the reverse. A CI job rejects anything else.
- File names are English in both trees. A new page gets the same name in
  `en/` and `de/`, or the language switch cannot find its counterpart.
- Never change a count by hand. `internal/metrics/docnumbers_test.go` derives
  the counts from the catalogue and checks `README.md` and `docs/`.
- `python -m mkdocs build --strict` fails on a dead link or anchor. CI runs it.
- Screenshots come from `tools/screenshots.ps1`, never by hand.

A new feature is done when it is in every place that applies:

| Change | Page |
|---|---|
| new measurement | `what-is-reported.md`; for a new source also `how-values-are-obtained.md` |
| new switch on a settings page | the matching page under `interface/`, with its default |
| new export target or field | `interface/export-and-display.md` and `export-targets.md` |
| new identifier or prefix | `identifiers.md` |
| new failure case | the symptom table in `diagnostics.md` |
| change to build or checks | `building.md` |

## When you find a trap

Stop at the first rung that holds:

1. A test that fails when the trap springs.
2. A check in `build.ps1 -Check`, if a test cannot reach it.
3. A comment at the code that explains it, if neither can.

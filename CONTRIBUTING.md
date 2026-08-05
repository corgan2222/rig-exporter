# Contributing

Thanks for looking. This is a small, opinionated project; the rules below are
what keep it small.

## The one rule that matters

**Karg ist in Ordnung, falsch nicht** — sparse is fine, wrong is not.

On a machine without a graphics card, reporting no GPU group at all is correct
behaviour. A number under the wrong name, in the wrong unit, or attributed to
the wrong card is not. When in doubt, report nothing.

Everything below follows from that.

## Getting set up

Go 1.26.5 or newer, windows/amd64, no CGO. Then:

```powershell
.\build.ps1 -Check
```

That checks `gofmt`, runs `go vet -unsafeptr=false`, `staticcheck` and the whole
test suite, and only then builds with `-H windowsgui -s -w`. If it is green,
your change is ready.

The tray icon is committed and is only redrawn on request, with `-Icon`. It has
a switch of its own because `go run` links an unsigned executable into the build
cache and runs it from there, which Microsoft Defender's heuristics report as
`Trojan:Win32/Sabsik` — a false positive, but not one worth triggering on every
check build.

`staticcheck` is not optional and must be from 2022 or newer — older releases
predate generics and produce pages of noise about the standard library instead
of anything about this code:

```powershell
go install honnef.co/go/tools/cmd/staticcheck@latest
```

Install the commit hook once, so the same checks run before a commit rather
than after a review:

```powershell
git config core.hooksPath .githooks
```

## How changes get in

**Only through a pull request.** `main` is protected; nothing is committed to
it directly, including by the maintainer.

Branch names carry meaning here — the release notes are drafted from them:

| Prefix | For |
|---|---|
| `feature/` | new behaviour |
| `fix/` | a defect |
| `docs/` | documentation only |
| `test/` | tests only |
| `chore/` | build, tooling, dependencies |

Commit messages are **English**, in the imperative, and explain *why* rather
than restating the diff. The existing history is the style guide: a subject line
under about 70 characters, a blank line, then prose that says what was wrong and
what the change assumes. Do not add co-authors.

## Things that will get a change sent back

**The measurement contract.** `internal/metrics/testdata/catalogue.txt` pins
every identifier, unit, kind, precision, group, panel, category and Prometheus
name. A test fails when it drifts. Adding a line is the clean shape of an
extension; renaming one orphans every Home Assistant entity, Prometheus rule and
InfluxDB dashboard built on it. If a change is intended:

```powershell
go test ./internal/metrics/ -update-catalogue
```

and say in the pull request which consumers have to be repointed.

**Identifiers never translate, names always do.** `Key()`, `ObjectID`,
`UniqueID` and `default_entity_id` know no language. The displayed name follows
the configured one, hardware prefix included.

**The output is identical across data sources.** Where a value came from
reaches no export — not JSON, not Prometheus, not InfluxDB, not MQTT. A
dashboard must never come to depend on which helper programs happen to run on a
given machine.

**A missing value is omitted, not zeroed.** A `0` asserts something. An absent
field does not.

**Measure rather than assume.** Claims without evidence stand out. Where it is
possible, check against real hardware or a real broker and put the numbers in
the pull request — but keep numbers *from your machine* out of the shipped
documentation and the interface, where they would read as a statement about
somebody else's PC.

## Traps worth knowing before you start

`windows.LazyProc.Call` **panics** on a missing symbol; there is no error value.
With `-H windowsgui` the tray then dies without a word. Resolve every entry
point with `Find()` before calling it.

`go build` without `-H windowsgui` produces a console binary, and `-probe`
behaves completely differently there than in the shipped build. Use
`build.ps1`.

The httptest-based tests occasionally fail with a loopback connection error
under machine load. That is known and not your change; re-run them on an idle
machine before going hunting.

## Reporting something

Issues are welcome, and `-probe` output is worth more than a description:

```powershell
.\rig-exporter.exe -probe
```

It also writes to `%APPDATA%\rig-exporter\probe.txt`, and its header records the
time, whether the process is elevated, and what PawnIO can do — all three decide
whole classes of measurements. **Read it before pasting**: it contains your
machine's name, drive labels and network addresses.

# Building it yourself

You do not need a finished release for this: the source builds on an ordinary
Windows machine in under a minute, and exactly one file falls out of it.

## What you need

| | |
|---|---|
| **Go** | 1.26 or newer. `go.mod` requires 1.26.5, the dependencies on their own 1.25 |
| **Git** | for the build stamp — it builds without Git too, just without a stamp |
| **A C compiler** | **no.** No CGO, no library, no toolchain zoo |

The target platform is `windows/amd64`. Other Windows architectures compile but
have never been run; outside Windows nothing compiles at all — the whole program
sits behind `//go:build windows`.

## Building

```powershell
.\build.ps1
```

The result is a single `rig-exporter.exe` of around 18 MB, with no accompanying
files. It is built with `-H windowsgui -s -w`: no console window, no debug
symbols.

!!! warning "`go build` on its own is not enough"

    Without `-H windowsgui` a console binary comes out. It starts, looks like it
    runs, and still behaves differently from the shipped program — `-probe`
    especially. If you build by hand, set the flag.

## Checking

```powershell
.\build.ps1 -Check
```

That is the same run the CI performs on every pull request, and it is the answer
to "does this go through": `gofmt`, `go vet -unsafeptr=false`, `staticcheck` with
all 149 checks from `staticcheck.conf`, every test — and only after that the
build. A red check run builds nothing.

```powershell
.\build.ps1 -Check -Race
```

The same, additionally under the race detector. It runs separately because it is
the only part of the project that needs **cgo** and roughly doubles the running
time. The script finds a mingw-w64 under `C:\msys64\ucrt64` or `…\mingw64` by
itself.

!!! danger "Not `C:\msys64\usr\bin\gcc`"

    That one builds against the MSYS runtime and is useless for Go.
    `CGO_ENABLED` is set for the test run only anyway and taken back afterwards —
    the shipped binary stays cgo-free.

And what a green run does **not** mean: that the code is free of races. It means
that the existing tests did not trigger one. A detector finds only what actually
happens.

## The build stamp

Behind the version stands where the binary came from:

```
rig-exporter 1.10.3+<commits>.<hash>
```

Commit count from `git rev-list --count`, short hash from `git rev-parse --short`,
and with uncommitted changes `.dirty` on top. Derived rather than maintained, so
that it cannot diverge from the code it describes in the first place.

The reason: a version number on its own never answers the question "is this the
binary with the fix" — between two commits it does not move. A plain `go build`
leaves the stamp empty, and that too is an honest statement: this binary did not
come out of the script.

Side effect you trip over once: **every uncommitted file marks the build
`.dirty`** — even one that is merely lying around.

## The icon

```powershell
.\build.ps1 -Icon
```

Only this regenerates the icons; otherwise they sit finished in the repository.
`tools/genicon` makes three things out of **one** source —
`docs/images/rig-exporter-entity-512.png`:

* `icon.ico` for the notification area
* `rsrc_windows_amd64.syso`, the Windows resource file that gives the exe its
  icon in Explorer, taskbar and Alt-Tab
* `icon.png`, which the web interface serves at `/icon.png`

Three images of the same program contradicting each other would be worse than
none.

That this is a flag of its own and does not run along with every build has a
solid reason: for it, `go run` links a fresh, unsigned binary into the build
cache and starts it from there — exactly the pattern Microsoft Defender
heuristically reports as `Trojan:Win32/Sabsik`. A virus find on every check run
is not worth the fright.

The resource file, by the way, is written by hand rather than generated with
`rsrc` or `goversioninfo`, and all three files are checked in — so that a bare
`go build` suffices without any extra tooling at all.

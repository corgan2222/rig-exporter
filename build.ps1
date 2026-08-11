# Builds rig-exporter.exe.
#
# -H windowsgui suppresses the console window, which is what makes it a proper
# tray application; -s -w strips the symbol table to keep the binary small.
#
#   .\build.ps1            # build
#   .\build.ps1 -Check     # gofmt, vet, staticcheck, test, then build
#   .\build.ps1 -Race      # run the tests under the race detector as well
#   .\build.ps1 -Icon      # draw internal/assets/icon.ico again as well

param(
    [switch]$Check,
    [switch]$Icon,
    [switch]$Race,
    [string]$Output = "rig-exporter.exe"
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# Behind a switch of its own rather than part of -Check, and not because it is
# slow. `go run` links a fresh, unsigned executable into the build cache and
# runs it from there, which is the exact shape Microsoft Defender's machine
# learning classifier calls Trojan:Win32/Sabsik. Every check build earned a
# severe-threat popup for a program that draws a picture out of the standard
# library. The icon is committed and changes about once a year, so regenerating
# it belongs where it is asked for.
if ($Icon) {
    Write-Host "==> generating icon" -ForegroundColor Cyan
    go run ./tools/genicon
    if ($LASTEXITCODE -ne 0) { throw "genicon failed" }
}

if ($Check) {
    Write-Host "==> gofmt" -ForegroundColor Cyan
    $unformatted = gofmt -l .
    if ($unformatted) {
        throw "unformatted files:`n$unformatted"
    }

    # unsafeptr is disabled on purpose: reading the RTSS shared memory means
    # converting a MapViewOfFile address, which vet cannot reason about.
    Write-Host "==> go vet" -ForegroundColor Cyan
    go vet -unsafeptr=false ./...
    # The same omission that once let a red test suite through: a native
    # program's exit code does not trip $ErrorActionPreference, so without this
    # line vet printed its findings and the build carried on regardless. It was
    # the commit hook, not this script, that caught a copied mutex.
    if ($LASTEXITCODE -ne 0) { throw "go vet found problems" }

    # staticcheck catches a different class than vet: misused standard library,
    # ineffective code, unused declarations, style that has drifted. Which
    # checks run is in staticcheck.conf, next to this file.
    #
    # Skipped with a warning when it is not installed, so somebody who just
    # cloned the repository still gets a binary. A stale one is worse than none
    # and is refused: the version shipped before Go 1.18 cannot parse generics
    # and fails with pages of nonsense about the standard library.
    Write-Host "==> staticcheck" -ForegroundColor Cyan
    $staticcheck = Get-Command staticcheck -ErrorAction SilentlyContinue
    if (-not $staticcheck) {
        Write-Host "   staticcheck not installed, skipping" -ForegroundColor Yellow
        Write-Host "   go install honnef.co/go/tools/cmd/staticcheck@latest" -ForegroundColor Yellow
    } else {
        $version = (& staticcheck -version) -replace '.*?(\d{4})\.\d+.*', '$1'
        if ([int]$version -lt 2022) {
            throw "staticcheck $version predates generics and cannot read this code; go install honnef.co/go/tools/cmd/staticcheck@latest"
        }
        & staticcheck ./...
        if ($LASTEXITCODE -ne 0) { throw "staticcheck found problems" }
    }

    Write-Host "==> go test" -ForegroundColor Cyan
    go test ./...
    # The exit code was not checked here, and every step above it does check.
    # A failing test therefore left -Check green, the binary was built anyway,
    # and CI reported success while two tests were red. Whatever this script
    # runs, it has to be able to say no.
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }
}

# The race detector, behind its own switch rather than inside -Check.
#
# It needs cgo, cgo needs a C compiler, and the shipped build deliberately has
# neither: CGO_ENABLED stays 0 everywhere else in this script, including the
# build below. Turning it on here changes nothing about the binary, because the
# detector only ever touches the test executables.
#
# Kept out of -Check because it roughly doubles the run and depends on a
# toolchain that is not required to build this program. It is what makes any
# claim about concurrency in this repository a measurement rather than a
# reading — see HANDOVER 5.18.
if ($Race) {
    Write-Host "==> go test -race" -ForegroundColor Cyan

    # MSYS2 ships several gcc builds and only the mingw-w64 ones work with Go;
    # the one under usr\bin links against the MSYS runtime and does not. UCRT is
    # preferred over the older MSVCRT flavour, and an existing gcc in PATH is
    # left alone.
    if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
        $toolchain = @("C:\msys64\ucrt64\bin", "C:\msys64\mingw64\bin") |
            Where-Object { Test-Path (Join-Path $_ "gcc.exe") } |
            Select-Object -First 1
        if (-not $toolchain) {
            throw "the race detector needs a mingw-w64 gcc; install one or add it to PATH"
        }
        Write-Host "   using $toolchain" -ForegroundColor DarkGray
        $env:PATH = "$toolchain;$env:PATH"
    }

    $env:CGO_ENABLED = "1"
    try {
        go test -race ./...
        if ($LASTEXITCODE -ne 0) { throw "go test -race found problems" }
    } finally {
        # Back off before the build below, which must stay cgo-free.
        $env:CGO_ENABLED = "0"
    }
}

# The build identifier: how many commits deep, and which one. Derived rather
# than kept in a file, so it cannot drift from the code it describes. A checkout
# without git, or with uncommitted changes, still builds — the identifier just
# says less.
$build = ""
if (Get-Command git -ErrorAction SilentlyContinue) {
    $count = (git rev-list --count HEAD 2>$null)
    $hash = (git rev-parse --short HEAD 2>$null)
    if ($count -and $hash) {
        $build = "$count.$hash"
        if ((git status --porcelain 2>$null)) { $build += ".dirty" }
    }
}

Write-Host "==> building $Output" -ForegroundColor Cyan
$ldflags = "-H windowsgui -s -w"
if ($build) { $ldflags += " -X github.com/corgan2222/rig-exporter/internal/config.Build=$build" }
go build -trimpath -ldflags $ldflags -o $Output .

$size = [math]::Round((Get-Item $Output).Length / 1MB, 1)
Write-Host "built $Output ($size MB)" -ForegroundColor Green

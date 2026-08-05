# Builds rig-exporter.exe.
#
# -H windowsgui suppresses the console window, which is what makes it a proper
# tray application; -s -w strips the symbol table to keep the binary small.
#
#   .\build.ps1            # build
#   .\build.ps1 -Check     # regenerate the icon, vet, test, then build

param(
    [switch]$Check,
    [string]$Output = "rig-exporter.exe"
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if ($Check) {
    Write-Host "==> generating icon" -ForegroundColor Cyan
    go run ./tools/genicon

    Write-Host "==> gofmt" -ForegroundColor Cyan
    $unformatted = gofmt -l .
    if ($unformatted) {
        throw "unformatted files:`n$unformatted"
    }

    # unsafeptr is disabled on purpose: reading the RTSS shared memory means
    # converting a MapViewOfFile address, which vet cannot reason about.
    Write-Host "==> go vet" -ForegroundColor Cyan
    go vet -unsafeptr=false ./...

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

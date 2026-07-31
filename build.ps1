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

    Write-Host "==> go test" -ForegroundColor Cyan
    go test ./...
}

Write-Host "==> building $Output" -ForegroundColor Cyan
go build -trimpath -ldflags "-H windowsgui -s -w" -o $Output .

$size = [math]::Round((Get-Item $Output).Length / 1MB, 1)
Write-Host "built $Output ($size MB)" -ForegroundColor Green

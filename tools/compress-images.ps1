<#
.SYNOPSIS
    Compresses every PNG under docs/images in place, through a self-hosted
    SnapOtter instance.

.DESCRIPTION
    Screenshots of a flat dark interface hold very few distinct colours, so a
    palette-quantising pass roughly halves them with no visible loss. Measured
    on docs/images/screenshots/de/export-influx.png: 23242 -> 11102 bytes, text
    still crisp, no banding in the card gradients.

    A file is only overwritten when the result is BOTH smaller by a worthwhile
    margin AND the same size in pixels. The second check is the important one:
    it is what stops a resize, a re-render or an error page from silently
    replacing a screenshot.

    Re-running is mostly, but not entirely, a no-op: a second pass over an
    already-compressed file still finds something to give (measured: -10% on a
    16px favicon, -17% on a 32px one), and quantising an already quantised
    image does lose a little each time. -MinSavingPercent is the guard — at the
    default of 20 a second pass leaves every screenshot alone. Lower it only
    for a one-off, and regenerate screenshots with tools\screenshots.ps1 rather
    than squeezing them twice.

.PARAMETER ApiKey
    Overrides the key lookup. Normally leave this empty and use the file or the
    environment variable — see below.

    The key is NEVER stored in this script. It is read from, in order:
      1. -ApiKey
      2. $env:SNAPOTTER_API_KEY
      3. tools/.image-api.key   (git-ignored, one line)

.EXAMPLE
    .\tools\compress-images.ps1 -DryRun
    .\tools\compress-images.ps1
    .\tools\compress-images.ps1 -Quality 70
#>
param(
    [string]$ApiBase = "http://192.168.2.177:1349",
    [string]$Root    = "$PSScriptRoot\..\docs\images",
    [ValidateRange(1, 100)][int]$Quality = 80,
    [int]$MinSavingPercent = 20,
    [string]$ApiKey,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

# ------------------------------------------------------------------- key ----

if (-not $ApiKey) { $ApiKey = $env:SNAPOTTER_API_KEY }
if (-not $ApiKey) {
    $keyFile = Join-Path $PSScriptRoot ".image-api.key"
    if (Test-Path $keyFile) { $ApiKey = (Get-Content $keyFile -Raw).Trim() }
}
if (-not $ApiKey) {
    throw @"
No API key. Provide one of:
  -ApiKey <key>
  `$env:SNAPOTTER_API_KEY = '<key>'
  a single line in tools\.image-api.key   (git-ignored — keep it that way)
"@
}

$headers = @{ Authorization = "Bearer $ApiKey" }

# ---------------------------------------------------------------- checks ----

# Fail early and clearly rather than once per file: an unreachable host or a
# rejected key produce the same "0 files compressed" otherwise.
try {
    Invoke-WebRequest -Uri "$ApiBase/api/docs/openapi.json" -TimeoutSec 10 -UseBasicParsing | Out-Null
} catch {
    throw "SnapOtter not reachable at $ApiBase — $($_.Exception.Message)"
}

# Not $root: PowerShell variable names are not case sensitive, so that is the
# same variable as the [string]$Root parameter. Assigning a PathInfo to it
# coerces it back to a string, .Path then yields $null, and the relative labels
# below come out with their drive letter chopped off.
$rootPath = (Resolve-Path $Root).Path
$files = Get-ChildItem -Path $rootPath -Recurse -Filter *.png | Sort-Object FullName
if (-not $files) { "No PNG found under $rootPath"; return }

"$($files.Count) PNG under $rootPath, quality $Quality$(if ($DryRun) { ' (dry run)' })"
""

function Get-PixelSize([string]$path) {
    $bmp = New-Object System.Drawing.Bitmap $path
    try { return "$($bmp.Width)x$($bmp.Height)" } finally { $bmp.Dispose() }
}

$savedTotal = 0
$changed = 0
$skipped = 0

foreach ($file in $files) {
    $before = $file.Length
    $label = $file.FullName.Substring($rootPath.Length + 1)

    try {
        $response = Invoke-WebRequest -Uri "$ApiBase/api/v1/tools/image/compress" `
            -Method Post -TimeoutSec 120 -UseBasicParsing -Headers $headers `
            -Form @{
                file     = $file
                settings = (@{ mode = "quality"; quality = $Quality } | ConvertTo-Json -Compress)
            }
        $job = $response.Content | ConvertFrom-Json
    } catch {
        "{0,-46} FAILED  {1}" -f $label, $_.Exception.Message
        $skipped++
        continue
    }

    $after = [int]$job.processedSize
    $saving = if ($before -gt 0) { [Math]::Round(100 - ($after / $before * 100), 1) } else { 0 }

    if ($saving -lt $MinSavingPercent) {
        "{0,-46} skip    {1,6:N1} KB  ({2}% saving)" -f $label, ($before / 1KB), $saving
        $skipped++
        continue
    }

    if ($DryRun) {
        "{0,-46} would   {1,6:N1} -> {2,6:N1} KB  (-{3}%)" -f $label, ($before / 1KB), ($after / 1KB), $saving
        $savedTotal += ($before - $after)
        $changed++
        continue
    }

    # Download beside the original, verify, then replace. Never write the
    # response straight over the source: a 401 or an error page would land in
    # the file and the screenshot would be gone.
    $temp = "$($file.FullName).compressed"
    try {
        Invoke-WebRequest -Uri "$ApiBase$($job.downloadUrl)" -Headers $headers `
            -OutFile $temp -TimeoutSec 120

        $sizeBefore = Get-PixelSize $file.FullName
        $sizeAfter  = Get-PixelSize $temp
        if ($sizeBefore -ne $sizeAfter) {
            throw "pixel size changed: $sizeBefore -> $sizeAfter"
        }
        if ((Get-Item $temp).Length -ge $before) {
            throw "result is not smaller on disk"
        }

        Move-Item $temp $file.FullName -Force
        "{0,-46} ok      {1,6:N1} -> {2,6:N1} KB  (-{3}%)" -f $label, ($before / 1KB), ((Get-Item $file.FullName).Length / 1KB), $saving
        $savedTotal += ($before - $after)
        $changed++
    } catch {
        "{0,-46} KEPT    {1}" -f $label, $_.Exception.Message
        $skipped++
    } finally {
        if (Test-Path $temp) { Remove-Item $temp -Force -ErrorAction SilentlyContinue }
    }
}

""
"{0} compressed, {1} left alone, {2:N0} KB saved" -f $changed, $skipped, ($savedTotal / 1KB)

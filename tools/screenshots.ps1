<#
.SYNOPSIS
    Captures the interface for the handbook — four pages per language, plus
    every single card of the export and measurements pages.

.DESCRIPTION
    Headless Edge against the running instance. The exporter has to be running;
    this script only changes the display language and puts it back afterwards.

    Output goes to docs/images/screenshots/<lang>/.

    Check which port the running instance actually listens on before starting.
    config.json is not proof: the web server falls back to a different port when
    the configured one is taken, and something else answering on the expected
    port looks exactly like success.

.EXAMPLE
    .\tools\screenshots.ps1 -Lang de
    .\tools\screenshots.ps1 -Lang en -BaseUrl http://127.0.0.1:8788
#>
param(
    [Parameter(Mandatory = $true)][ValidateSet("de", "en")][string]$Lang,
    [string]$BaseUrl = "http://127.0.0.1:8788",
    [string]$OutRoot = "$PSScriptRoot\..\docs\images\screenshots",
    [switch]$KeepLanguage      # leave the interface in $Lang instead of restoring
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$edge = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
if (-not (Test-Path $edge)) { throw "Edge not found: $edge" }

# The width of an ordinary browser window. The height is deliberately far too
# large: the export page runs to about 4900 pixels, and Edge photographs the
# viewport only — whatever does not fit is simply missing from the image, with
# nothing to show that it was cut. Trim-Bottom removes what stays empty.
$WIDTH  = 1440
$HEIGHT = 5600

# The pages of the header bar, in their order.
$pages = [ordered]@{
    "dashboard"    = "/"
    "capture"      = "/capture"
    "measurements" = "/measurements"
    "export"       = "/export"
}

# Cards that are additionally saved on their own. The identifiers are the DOM
# ids of the cards and do not change with the language — unlike their height,
# which is why their position is measured rather than written down here.
$boxes = @{
    # The dashboard names only its first card. The list is "the cards to save,
    # from the top" rather than "every card on the page" — the hardware panels
    # below it are long, machine-specific and already covered by the full-page
    # shot, while the status card is the one a page wants to show on its own.
    "dashboard"    = @("status")

    "export"       = @("mqtt", "ha", "recorder", "data", "influx", "app", "logs")
    "measurements" = @("rung", "node-core", "node-gpu", "node-cpu", "node-ram", "node-disk", "node-net", "node-cooling")
}

# Pages whose FIRST card is saved together with everything above it. Everything
# else is cut to the card alone.
$withHeader = @("dashboard")

function Invoke-UI([string]$path, [string]$body) {
    $uri = [Uri]"$BaseUrl$path"
    $client = New-Object System.Net.Sockets.TcpClient($uri.Host, $uri.Port)
    $stream = $client.GetStream()
    # A raw socket rather than Invoke-WebRequest: the check in samesite.go lets
    # a request without Origin and without Sec-Fetch-Site through on purpose.
    $req = "POST $path HTTP/1.1`r`nHost: $($uri.Host):$($uri.Port)`r`n" +
           "Content-Type: application/x-www-form-urlencoded`r`n" +
           "Content-Length: $($body.Length)`r`nConnection: close`r`n`r`n$body"
    $bytes = [Text.Encoding]::ASCII.GetBytes($req)
    $stream.Write($bytes, 0, $bytes.Length); $stream.Flush()
    $response = (New-Object IO.StreamReader($stream)).ReadToEnd()
    $client.Close()
    return ($response -split "`r`n")[0]
}

function Get-Shot([string]$url, [string]$file) {
    # A fresh profile per capture. The interface remembers collapsed cards in
    # the browser's localStorage, so a shared profile hands back the second page
    # collapsed — and therefore far too short.
    $profileDir = Join-Path $env:TEMP "rig-shot-$([Guid]::NewGuid().ToString('N').Substring(0,8))"
    $flags = @(
        "--headless", "--disable-gpu", "--no-first-run",
        "--user-data-dir=$profileDir", "--hide-scrollbars",
        "--virtual-time-budget=8000", "--window-size=$WIDTH,$HEIGHT",
        "--screenshot=$file", $url
    )
    Start-Process -FilePath $edge -ArgumentList $flags -Wait -NoNewWindow | Out-Null
    Remove-Item $profileDir -Recurse -Force -ErrorAction SilentlyContinue
    return (Test-Path $file)
}

function Get-LastContentRow([System.Drawing.Bitmap]$bmp) {
    $bg = $bmp.GetPixel(4, $bmp.Height - 4)
    for ($y = $bmp.Height - 1; $y -ge 0; $y--) {
        for ($x = 0; $x -lt $bmp.Width; $x += 9) {
            $p = $bmp.GetPixel($x, $y)
            if ([Math]::Abs($p.R - $bg.R) -gt 14 -or
                [Math]::Abs($p.G - $bg.G) -gt 14 -or
                [Math]::Abs($p.B - $bg.B) -gt 14) { return $y }
        }
    }
    return 0
}

# Finds the cards by probing a column inside their left padding: no text ever
# sits there, and the card surface is a shade lighter than the page behind it.
# Measured rather than tabulated, because German text is longer and puts every
# card somewhere else.
function Get-CardBands([System.Drawing.Bitmap]$bmp) {
    $bg = $bmp.GetPixel(4, 4)
    $probe = 290          # 272 = left card edge, plus its padding
    # Objects rather than pairs of numbers: PowerShell unrolls nested arrays on
    # return, and a list of pairs quietly turns into something else.
    $bands = New-Object System.Collections.ArrayList
    $start = -1
    for ($y = 0; $y -lt $bmp.Height; $y++) {
        $p = $bmp.GetPixel($probe, $y)
        $onCard = ([Math]::Abs($p.R - $bg.R) -gt 3 -or
                   [Math]::Abs($p.G - $bg.G) -gt 3 -or
                   [Math]::Abs($p.B - $bg.B) -gt 3)
        if ($onCard -and $start -lt 0) { $start = $y }
        elseif (-not $onCard -and $start -ge 0) {
            if (($y - $start) -ge 80) {
                [void]$bands.Add([PSCustomObject]@{ Top = $start; Height = $y - $start })
            }
            $start = -1
        }
    }
    if ($start -ge 0 -and ($bmp.Height - $start) -ge 80) {
        [void]$bands.Add([PSCustomObject]@{ Top = $start; Height = $bmp.Height - $start })
    }
    return , $bands
}

function Save-Crop([System.Drawing.Bitmap]$bmp, [int]$top, [int]$cardHeight, [string]$file) {
    $pad = 12
    $y = [Math]::Max(0, $top - $pad)
    $h = [Math]::Min($bmp.Height - $y, $cardHeight + 2 * $pad)
    $rect = New-Object System.Drawing.Rectangle 240, $y, 960, $h   # card plus a margin
    $crop = $bmp.Clone($rect, $bmp.PixelFormat)
    $crop.Save($file, [System.Drawing.Imaging.ImageFormat]::Png)
    $crop.Dispose()
}

# Save-TopCrop keeps everything above the card as well: the logo, the version,
# the navigation and the language switch, at the full width of the page.
#
# A card lifted out on its own says nothing about where it lives. For the first
# card of a page that is exactly the information the picture is for — the reader
# has to see which of the four pages they are being shown.
function Save-TopCrop([System.Drawing.Bitmap]$bmp, [int]$bottom, [string]$file) {
    $h = [Math]::Min($bmp.Height, $bottom + 12)
    $rect = New-Object System.Drawing.Rectangle 0, 0, $bmp.Width, $h
    $crop = $bmp.Clone($rect, $bmp.PixelFormat)
    $crop.Save($file, [System.Drawing.Imaging.ImageFormat]::Png)
    $crop.Dispose()
}

# ------------------------------------------------------------------- run ----

$outDir = Join-Path (Resolve-Path $OutRoot) $Lang
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$previous = $null
try {
    $status = Invoke-UI "/language" "lang=$Lang"
    if ($status -notmatch "30[0-9]|200") { throw "Language switch failed: $status" }
    $previous = if ($Lang -eq "de") { "en" } else { "de" }
    Start-Sleep -Milliseconds 800

    foreach ($name in $pages.Keys) {
        $file = Join-Path $outDir "$name.png"
        if (-not (Get-Shot "$BaseUrl$($pages[$name])" $file)) {
            Write-Warning "$name : no file produced"
            continue
        }

        $bmp = New-Object System.Drawing.Bitmap $file

        if ($boxes.ContainsKey($name)) {
            $bands = Get-CardBands $bmp
            $ids = $boxes[$name]
            # Fewer cards than names is a fault: a card the page should have is
            # missing, or the detection lost one. More is not — a page may
            # deliberately name only its first few, as the dashboard does.
            if ($bands.Count -lt $ids.Count) {
                Write-Warning "$name : found $($bands.Count) cards, expected at least $($ids.Count) — check the mapping"
            }
            for ($i = 0; $i -lt [Math]::Min($bands.Count, $ids.Count); $i++) {
                # NOT $file: the page's own path is already in $file, and the
                # trim below writes to it. Overwriting it here put the trimmed
                # full page into the card's file and left the page untrimmed.
                $cardFile = Join-Path $outDir "$name-$($ids[$i]).png"
                if ($i -eq 0 -and $withHeader -contains $name) {
                    Save-TopCrop $bmp ($bands[$i].Top + $bands[$i].Height) $cardFile
                } else {
                    Save-Crop $bmp $bands[$i].Top $bands[$i].Height $cardFile
                }
            }
            "{0,-16} {1} cards" -f $name, $bands.Count
        }

        # Trim the full page last, so the cards are cut from the untrimmed image.
        #
        # Do NOT name this $height. PowerShell variable names are not case
        # sensitive, so it would overwrite $HEIGHT above — and every page after
        # the first is then captured at the trimmed height of the one before it,
        # which looks like a set of short pages rather than a bug.
        $last = Get-LastContentRow $bmp
        $trimHeight = [Math]::Min($bmp.Height, $last + 28)
        if ($trimHeight -lt $bmp.Height -and $trimHeight -gt 200) {
            $rect = New-Object System.Drawing.Rectangle 0, 0, $bmp.Width, $trimHeight
            $crop = $bmp.Clone($rect, $bmp.PixelFormat)
            $bmp.Dispose()
            Remove-Item $file -Force
            $crop.Save($file, [System.Drawing.Imaging.ImageFormat]::Png)
            $crop.Dispose()
        } else {
            $bmp.Dispose()
        }
        "{0,-16} {1} x {2}" -f "$name.png", $WIDTH, $trimHeight
    }
}
finally {
    if ($previous -and -not $KeepLanguage) {
        # After a failure as well: otherwise the interface stays in whatever
        # language was captured last.
        Invoke-UI "/language" "lang=$previous" | Out-Null
    }
}

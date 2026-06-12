param(
    [string]$BinDir = "bin",
    [switch]$RequireMediaRuntime
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$bin = Join-Path $root $BinDir
$runtime = Join-Path $root "build\windows\runtime"
$licenseOut = Join-Path $bin "licenses"

New-Item -ItemType Directory -Force -Path $bin | Out-Null
New-Item -ItemType Directory -Force -Path $licenseOut | Out-Null

function Copy-TreeIfPresent {
    param(
        [Parameter(Mandatory=$true)][string]$Source,
        [Parameter(Mandatory=$true)][string]$Destination
    )
    if (Test-Path $Source) {
        if (Test-Path $Destination) {
            Remove-Item -LiteralPath $Destination -Recurse -Force
        }
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
        Copy-Item -LiteralPath $Source -Destination $Destination -Recurse -Force
    }
}

function Copy-FileIfPresent {
    param(
        [Parameter(Mandatory=$true)][string]$Source,
        [Parameter(Mandatory=$true)][string]$Destination
    )
    if (Test-Path $Source) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    }
}

Copy-TreeIfPresent -Source (Join-Path $runtime "ffmpeg") -Destination (Join-Path $bin "runtime\ffmpeg")
Copy-TreeIfPresent -Source (Join-Path $runtime "licenses") -Destination (Join-Path $licenseOut "media-runtime")

Get-ChildItem -Path $runtime -Filter "*.dll" -File -ErrorAction SilentlyContinue | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $bin $_.Name) -Force
}

foreach ($mpvDir in @((Join-Path $runtime "mpv"), (Join-Path $runtime "mpv\bin"))) {
    Get-ChildItem -Path $mpvDir -Filter "*.dll" -File -ErrorAction SilentlyContinue | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $bin $_.Name) -Force
    }
}

Copy-FileIfPresent -Source (Join-Path $root "LICENSE") -Destination (Join-Path $licenseOut "LICENSE")
Copy-FileIfPresent -Source (Join-Path $root "THIRD_PARTY_NOTICES.md") -Destination (Join-Path $licenseOut "THIRD_PARTY_NOTICES.md")
Copy-FileIfPresent -Source (Join-Path $root "docs\dependency-sources.md") -Destination (Join-Path $licenseOut "dependency-sources.md")
Copy-FileIfPresent -Source (Join-Path $root "build\deps\manifest.json") -Destination (Join-Path $licenseOut "dependency-manifest.json")

if ($RequireMediaRuntime) {
    $required = @(
        (Join-Path $bin "runtime\ffmpeg\bin\ffmpeg.exe"),
        (Join-Path $bin "runtime\ffmpeg\bin\ffprobe.exe"),
        (Join-Path $bin "libmpv.dll"),
        (Join-Path $licenseOut "LICENSE"),
        (Join-Path $licenseOut "THIRD_PARTY_NOTICES.md"),
        (Join-Path $licenseOut "dependency-sources.md"),
        (Join-Path $licenseOut "dependency-manifest.json")
    )
    $missing = @($required | Where-Object { -not (Test-Path $_) })
    if ($missing.Count -gt 0) {
        throw "Release media runtime is incomplete. Missing: $($missing -join ', ')"
    }
}

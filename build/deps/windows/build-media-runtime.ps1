param(
    [string]$MsysRoot = "C:\msys64"
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$bash = Join-Path $MsysRoot "usr\bin\bash.exe"
if (-not (Test-Path $bash)) {
    throw "MSYS2 bash was not found at $bash"
}

function Convert-ToMsysPath {
    param([Parameter(Mandatory=$true)][string]$Path)
    $resolved = (Resolve-Path $Path).Path
    if ($resolved -match '^([A-Za-z]):\\(.*)$') {
        $drive = $matches[1].ToLowerInvariant()
        $tail = $matches[2] -replace '\\','/'
        return "/$drive/$tail"
    }
    return ($resolved -replace '\\','/')
}

$rootMsys = Convert-ToMsysPath $root
$env:MSYSTEM = "MINGW64"
$env:CHERE_INVOKING = "1"
& $bash -lc "export MSYSTEM=MINGW64; export PATH=/mingw64/bin:/usr/bin:`$PATH; cd '$rootMsys' && ./build/deps/windows/build-media-runtime.mingw64.sh"
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

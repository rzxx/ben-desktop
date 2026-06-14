param(
    [string]$BinDir = "bin"
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..\..")
if ([System.IO.Path]::IsPathRooted($BinDir)) {
    $bin = $BinDir
} else {
    $bin = Join-Path $root $BinDir
}
$bin = (Resolve-Path $bin).Path

function Get-MinimalPath {
    $systemRoot = $env:SystemRoot
    if ([string]::IsNullOrWhiteSpace($systemRoot)) {
        $systemRoot = "C:\Windows"
    }
    return "$systemRoot\System32;$systemRoot"
}

function Get-PowerShellExe {
    foreach ($name in @("pwsh", "powershell")) {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if ($cmd -and $cmd.Source) {
            return $cmd.Source
        }
    }
    throw "Could not find pwsh or powershell for isolated runtime validation"
}

function Invoke-IsolatedPowerShell {
    param(
        [Parameter(Mandatory=$true)][string]$Script,
        [Parameter(Mandatory=$true)][string]$Description
    )

    $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($Script))
    $psi = [System.Diagnostics.ProcessStartInfo]::new((Get-PowerShellExe), "-NoProfile -NonInteractive -EncodedCommand $encoded")
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.Environment["PATH"] = Get-MinimalPath

    $process = [System.Diagnostics.Process]::Start($psi)
    if (-not $process.WaitForExit(15000)) {
        try {
            $process.Kill()
        } catch {
        }
        throw "$Description timed out"
    }

    $stdout = $process.StandardOutput.ReadToEnd().Trim()
    $stderr = $process.StandardError.ReadToEnd().Trim()
    if ($process.ExitCode -ne 0) {
        $message = "$Description failed with exit code $($process.ExitCode)"
        if (-not [string]::IsNullOrWhiteSpace($stdout)) {
            $message += "`nstdout: $stdout"
        }
        if (-not [string]::IsNullOrWhiteSpace($stderr)) {
            $message += "`nstderr: $stderr"
        }
        throw $message
    }
}

function Test-LibmpvLoad {
    $escapedBin = $bin.Replace("'", "''")
    $script = @"
`$ErrorActionPreference = 'Stop'
Set-Location '$escapedBin'
`$env:PATH = "$(Get-MinimalPath)"
Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class Native { [DllImport("kernel32", SetLastError=true, CharSet=CharSet.Unicode)] public static extern bool SetDllDirectory(string lpPathName); [DllImport("kernel32", SetLastError=true, CharSet=CharSet.Unicode)] public static extern IntPtr LoadLibrary(string lpFileName); }'
[void][Native]::SetDllDirectory((Get-Location).Path)
`$handle = [Native]::LoadLibrary((Join-Path (Get-Location) 'libmpv.dll'))
if (`$handle -eq [IntPtr]::Zero) {
    Write-Output ("LoadLibrary failed with Win32 error " + [Runtime.InteropServices.Marshal]::GetLastWin32Error())
    exit 1
}
"@
    Invoke-IsolatedPowerShell -Script $script -Description "libmpv.dll load validation"
    Write-Host "Validated libmpv.dll loads from $bin"
}

function Test-RuntimeProgram {
    param(
        [Parameter(Mandatory=$true)][string]$Path,
        [Parameter(Mandatory=$true)][string]$Name
    )

    if (-not (Test-Path -LiteralPath $Path)) {
        throw "$Name is missing at $Path"
    }

    $psi = [System.Diagnostics.ProcessStartInfo]::new($Path, "-version")
    $psi.WorkingDirectory = Split-Path -Parent $Path
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.Environment["PATH"] = Get-MinimalPath

    try {
        $process = [System.Diagnostics.Process]::Start($psi)
    } catch {
        throw "$Name failed to start: $($_.Exception.Message)"
    }

    if (-not $process.WaitForExit(15000)) {
        try {
            $process.Kill()
        } catch {
        }
        throw "$Name -version timed out"
    }

    $stdout = $process.StandardOutput.ReadToEnd().Trim()
    $stderr = $process.StandardError.ReadToEnd().Trim()
    if ($process.ExitCode -ne 0) {
        $message = "$Name -version failed with exit code $($process.ExitCode)"
        if (-not [string]::IsNullOrWhiteSpace($stdout)) {
            $message += "`nstdout: $stdout"
        }
        if (-not [string]::IsNullOrWhiteSpace($stderr)) {
            $message += "`nstderr: $stderr"
        }
        throw $message
    }
    Write-Host "Validated $Name runs from $(Split-Path -Parent $Path)"
}

Test-LibmpvLoad
Test-RuntimeProgram -Path (Join-Path $bin "runtime\ffmpeg\bin\ffmpeg.exe") -Name "ffmpeg.exe"
Test-RuntimeProgram -Path (Join-Path $bin "runtime\ffmpeg\bin\ffprobe.exe") -Name "ffprobe.exe"

[CmdletBinding()]
param(
    [ValidateSet('prepare', 'sync', 'hooks', 'configure', 'build', 'verify', 'package-windows')]
    [string]$StartStage = 'prepare',
    [ValidateRange(1, 8)]
    [int]$Jobs = 4
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows -or $env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
    throw 'The Chromium baseline requires a native Windows x64 runner.'
}

function Invoke-Checked {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command failed with exit code $LASTEXITCODE" }
}

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ($root.Contains(' ')) { throw 'Chromium requires a workspace path without spaces.' }
$memory = (Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB
if ($memory -lt 15) { throw 'Provision at least 16 GB RAM; 32 GB or more is recommended.' }
if ($Jobs -gt [math]::Floor($memory / 4)) { throw 'Allow at least 4 GB RAM per compiler job.' }
$disk = Get-PSDrive -Name ([System.IO.Path]::GetPathRoot($root).Substring(0, 1))
$source = Join-Path $root '.tools/chromium/workspace/src'
$reserve = if (Test-Path (Join-Path $source '.git')) { 20GB } else { 100GB }
if ($disk.Free -lt $reserve) { throw 'Insufficient disk space. Provision a 300 GB or larger SSD.' }

$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
if (-not (Test-Path $vswhere)) { throw 'Install Visual Studio 2026 with C++ and ATL/MFC support.' }
$vs = & $vswhere -latest -version '[18.0,19.0)' -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 Microsoft.VisualStudio.Component.VC.ATLMFC -property installationPath
if (-not $vs) { throw 'The pinned Chromium revision requires Visual Studio 2026 with C++ and ATL/MFC.' }
$sdkRoot = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows Kits\Installed Roots').KitsRoot10
if (-not (Test-Path (Join-Path $sdkRoot 'Include/10.0.28000.0/um/Windows.h'))) {
    throw 'Install Windows SDK 10.0.28000.2270 (headers under 10.0.28000.0), as required by the pinned Chromium source.'
}
$env:vs2026_install = $vs.Trim()
$env:DEPOT_TOOLS_WIN_TOOLCHAIN = '0'
$env:DEPOT_TOOLS_UPDATE = '0'
$env:GIT_TERMINAL_PROMPT = '0'

Push-Location $root
try {
    $python = (Get-Command python.exe -ErrorAction Stop).Source
    Invoke-Checked $python @('-m', 'unittest', 'discover', '-s', 'browser-engine', '-p', 'test_*.py')
    $stages = @('prepare', 'sync', 'hooks', 'configure', 'build', 'verify', 'package-windows')
    $start = [array]::IndexOf($stages, $StartStage)
    foreach ($stage in $stages[$start..($stages.Length - 1)]) {
        if ($stage -eq 'prepare') {
            Invoke-Checked $python @('browser-engine/prepare_windows.py')
            Invoke-Checked $python @('browser-engine/bootstrap.py', 'tools', '--jobs', "$Jobs")
            Invoke-Checked $python @('browser-engine/bootstrap.py', 'fetch', '--jobs', "$Jobs")
        } else {
            Invoke-Checked $python @('browser-engine/bootstrap.py', $stage, '--jobs', "$Jobs")
        }
    }
} finally { Pop-Location }

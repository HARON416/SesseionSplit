[CmdletBinding()]
param([ValidateSet('standard', 'bundled-tools')][string]$InstallerFlavor = 'standard')

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'Run this script on a Windows x64 runner using PowerShell 7.' }

function Invoke-Checked {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command failed with exit code $LASTEXITCODE" }
}

$root = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
Push-Location $root
try {
    $env:CGO_ENABLED = '0'
    $env:GOARCH = 'amd64'
    Invoke-Checked npm.cmd @('--prefix', 'frontend', 'ci')
    Invoke-Checked npm.cmd @('--prefix', 'frontend', 'run', 'check')
    Invoke-Checked npm.cmd @('--prefix', 'frontend', 'test')
    Invoke-Checked npm.cmd @('--prefix', 'frontend', 'run', 'build')
    Invoke-Checked go @('test', './...')

    $versionLine = Get-Content go.mod | Where-Object { $_ -match '^\s*github\.com/wailsapp/wails/v3\s+v' } | Select-Object -First 1
    if (-not $versionLine) { throw 'Cannot find the pinned Wails version in go.mod.' }
    $version = ($versionLine.Trim() -split '\s+')[1]
    $tools = Join-Path $root '.tools/bin'
    New-Item -ItemType Directory -Force $tools | Out-Null
    $env:GOBIN = $tools
    Invoke-Checked go @('install', "github.com/wailsapp/wails/v3/cmd/wails3@$version")
    $wails = Join-Path $tools 'wails3.exe'
    Invoke-Checked $wails @('generate', 'icons', '-input', 'build/appicon.png', '-windowsfilename', 'build/windows/icon.ico', '-macfilename', 'build/darwin/icons.icns')
    $syso = Join-Path $root 'wails_windows_amd64.syso'
    try {
        Invoke-Checked $wails @('generate', 'syso', '-arch', 'amd64', '-icon', 'build/windows/icon.ico', '-manifest', 'build/windows/wails.exe.manifest', '-info', 'build/windows/info.json', '-out', $syso)
        Invoke-Checked go @('build', '-tags', 'production', '-trimpath', '-buildvcs=false', '-ldflags=-w -s -H windowsgui', '-o', 'bin/isolate.exe', '.')
    } finally {
        if (Test-Path $syso) { Remove-Item -LiteralPath $syso }
    }

    $binary = (Resolve-Path -LiteralPath (Join-Path (Join-Path $root 'bin') 'isolate.exe')).Path
    if ((Get-Item -LiteralPath $binary).Length -eq 0) { throw 'The application executable is empty.' }

    $bundleArgument = @()
    $installerName = 'isolate-amd64-installer.exe'
    if ($InstallerFlavor -eq 'bundled-tools') {
        Invoke-Checked go @('run', './build/runtime-bundle', '-out', '.tools/runtime/windows-amd64')
        $bundle = (Resolve-Path -LiteralPath '.tools/runtime/windows-amd64').Path
        $bundleArgument = @("/DISOLATE_RUNTIME_BUNDLE=$bundle")
        $installerName = 'isolate-amd64-bundled-tools-installer.exe'
    }
    $nsis = Join-Path $root 'build/windows/nsis'
    Invoke-Checked $wails @('generate', 'webview2bootstrapper', '-dir', $nsis)
    Push-Location $nsis
    try {
        Invoke-Checked makensis ($bundleArgument + @('/DINFO_PROJECTNAME=isolate', '/DINFO_PRODUCTNAME=Isolate', '/DINFO_COMPANYNAME=Isolate', '/DPRODUCT_EXECUTABLE=isolate.exe', '/DWAILS_INSTALL_SCOPE=user', '/DREQUEST_EXECUTION_LEVEL=user', "/DARG_WAILS_AMD64_BINARY=$binary", 'project.nsi'))
    } finally { Pop-Location }

    $installer = Join-Path (Join-Path $root 'bin') $installerName
    if (-not (Test-Path $installer)) { throw 'NSIS did not produce the expected installer.' }
    $hash = (Get-FileHash -Algorithm SHA256 $installer).Hash.ToLowerInvariant()
    "$hash  $installerName" | Set-Content -Encoding ascii bin/SHA256SUMS.txt
    $commit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot record application source revision.' }
    @{
        schema_version = 1
        source_revision = $commit
        built_at = [DateTime]::UtcNow.ToString('o')
        target = 'windows/amd64'
        installer_flavor = $InstallerFlavor
        android_download_required = $true
        wails_version = $version
        installer_sha256 = $hash
        signed = $false
        browser_requirement = 'Supported installed browser (Chromium-based or Firefox)'
    } | ConvertTo-Json | Set-Content -Encoding utf8 bin/windows-build.json
} finally { Pop-Location }

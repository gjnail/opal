# opal installer for Windows (Windows PowerShell 5.1 or PowerShell 7).
#
#   From a checkout (builds with Go):  .\install\install.ps1
#   From a release:                    irm https://raw.githubusercontent.com/gjnail/opal/main/install/install.ps1 | iex
#
# Installs to %LOCALAPPDATA%\opal\bin, adds that to your user PATH, then runs
# `opal setup` to hook PowerShell (5.1 and 7) and Git Bash. Undo with:
#   opal setup --remove
param([switch]$NoSetup)
$ErrorActionPreference = 'Stop'

$repo = if ($env:OPAL_REPO) { $env:OPAL_REPO } else { 'gjnail/opal' }
$dir = Join-Path $env:LOCALAPPDATA 'opal\bin'
$exe = Join-Path $dir 'opal.exe'
New-Item -ItemType Directory -Force $dir | Out-Null

$checkout = if ($PSScriptRoot) { Split-Path $PSScriptRoot -Parent } else { $null }
$isCheckout = $checkout -and (Select-String -Path (Join-Path $checkout 'go.mod') -Pattern '^module opal$' -Quiet -ErrorAction SilentlyContinue)
if ($isCheckout -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Building opal from $checkout ..."
    Push-Location $checkout
    # Stamp the checkout path in so `opal update` can rebuild from it.
    try { go build -trimpath -ldflags "-X 'opal/internal/cli.SourceDir=$checkout'" -o $exe . } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} else {
    $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
    $name = "opal_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/latest/download"
    Write-Host "Downloading $base/$name ..."
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("opal-" + [guid]::NewGuid())
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile "$tmp.zip"
    $sums = (Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS.txt").Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $want = ($sums -split "`n" | Where-Object { ($_ -split '\s+')[1] -in @($name, "*$name") } | ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    $got = (Get-FileHash -Algorithm SHA256 "$tmp.zip").Hash
    if (-not $want -or $want -ne $got) {
        Remove-Item "$tmp.zip" -Force
        throw "$name doesn't match SHA256SUMS.txt; not installing it"
    }
    Expand-Archive "$tmp.zip" -DestinationPath $tmp -Force
    Copy-Item (Get-ChildItem $tmp -Recurse -Filter opal.exe | Select-Object -First 1).FullName $exe -Force
    Remove-Item "$tmp.zip", $tmp -Recurse -Force
}

# Add to the user PATH. Edit the registry value directly so entries like
# %USERPROFILE%\bin keep expanding (SetEnvironmentVariable would flatten them).
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$raw = [string]$key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
if (($raw -split ';') -notcontains $dir) {
    $key.SetValue('Path', (($dir, $raw) -join ';').TrimEnd(';'), 'ExpandString')
    # Nudge Explorer and new terminals to re-read the environment.
    [Environment]::SetEnvironmentVariable('OPAL_PATH_REFRESH', '1', 'User')
    [Environment]::SetEnvironmentVariable('OPAL_PATH_REFRESH', $null, 'User')
    Write-Host "Added $dir to your user PATH."
}
$key.Close()
if (($env:Path -split ';') -notcontains $dir) { $env:Path = "$dir;$env:Path" }

& $exe version
if (-not $NoSetup) { & $exe setup }

# opal installer for Windows (Windows PowerShell 5.1 or PowerShell 7).
#
#   From a checkout (builds with Go):  .\install\install.ps1
#   From a release:                    irm https://raw.githubusercontent.com/gjnail/opal/main/install/install.ps1 | iex
#
# Installs to %LOCALAPPDATA%\opal\bin, adds that to your user PATH, then runs
# `opal setup` to hook PowerShell (5.1 and 7) and Git Bash. Undo with:
#   opal setup --remove
#
# It also installs Opal Terminal, the terminal app, to
# %LOCALAPPDATA%\opal\terminal with a Start menu shortcut. -NoTerminal (or
# OPAL_NO_TERMINAL=1) skips that; to remove it, delete that folder and the
# shortcut.
param([switch]$NoSetup, [switch]$NoTerminal)
$ErrorActionPreference = 'Stop'

$repo = if ($env:OPAL_REPO) { $env:OPAL_REPO } else { 'gjnail/opal' }
$dir = Join-Path $env:LOCALAPPDATA 'opal\bin'
$exe = Join-Path $dir 'opal.exe'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
New-Item -ItemType Directory -Force $dir | Out-Null
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# Downloads a file from the latest release to $dest and checks it against
# the release's checksum file.
function Get-ReleaseFile([string]$name, [string]$sumsName, [string]$dest) {
    $base = "https://github.com/$repo/releases/latest/download"
    Write-Host "Downloading $base/$name ..."
    Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile $dest
    $sums = (Invoke-WebRequest -UseBasicParsing "$base/$sumsName").Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $want = ($sums -split "`n" | Where-Object { ($_ -split '\s+')[1] -in @($name, "*$name") } | ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    $got = (Get-FileHash -Algorithm SHA256 $dest).Hash
    if (-not $want -or $want -ne $got) {
        Remove-Item $dest -Force
        throw "$name doesn't match $sumsName; not installing it"
    }
}

$checkout = if ($PSScriptRoot) { Split-Path $PSScriptRoot -Parent } else { $null }
$isCheckout = $checkout -and (Select-String -Path (Join-Path $checkout 'go.mod') -Pattern '^module opal$' -Quiet -ErrorAction SilentlyContinue)
if ($isCheckout -and (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Building opal from $checkout ..."
    Push-Location $checkout
    # Stamp the checkout path in so `opal update` can rebuild from it.
    try { go build -trimpath -ldflags "-X 'opal/internal/cli.SourceDir=$checkout'" -o $exe . } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
} else {
    $name = "opal_windows_$arch.zip"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("opal-" + [guid]::NewGuid())
    Get-ReleaseFile $name 'SHA256SUMS.txt' "$tmp.zip"
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

# Opal Terminal needs Go 1.26 to build (the opal CLI builds with older
# versions). Checked from the temp folder: inside a module that asks for a
# newer Go, `go env` would download that toolchain first.
function Test-GoForTerminal {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { return $false }
    Push-Location ([IO.Path]::GetTempPath())
    try { $v = [string](go env GOVERSION) } finally { Pop-Location }
    $m = [regex]::Match($v, '^go(\d+)\.(\d+)')
    return $m.Success -and (([int]$m.Groups[1].Value -gt 1) -or ([int]$m.Groups[2].Value -ge 26))
}

# Builds Opal Terminal from the checkout into $out. Returns $false if the
# build fails, so the release download can take over.
function Build-OpalTerminal([string]$out) {
    $src = Join-Path $checkout 'terminal'
    Write-Host "Building Opal Terminal from $src ..."
    Push-Location $src
    try {
        # The icon, manifest and version info. Without them it still runs.
        # (Out-Host keeps their output out of this function's return value.)
        go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --arch $arch | Out-Host
        if ($LASTEXITCODE -ne 0) { Write-Warning 'go-winres failed; Opal Terminal will have no icon.' }
        go build -trimpath -ldflags '-H=windowsgui' -o (Join-Path $out 'opal-terminal.exe') . | Out-Host
        $ok = $LASTEXITCODE -eq 0
        Get-ChildItem -Filter 'rsrc_windows_*.syso' | Remove-Item -Force
    } finally { Pop-Location }
    if (-not $ok) {
        Write-Warning 'Building Opal Terminal failed; trying the release download.'
        return $false
    }
    # Microsoft's ConPTY passes image sequences through; the one built into
    # Windows drops them. Opal Terminal works without it.
    $conptyArch = if ($arch -eq 'arm64') { 'arm64' } else { 'x64' }
    try { & (Join-Path $src 'scripts\fetch-conpty.ps1') -Dest $out -Arch $conptyArch | Out-Host }
    catch { Write-Warning "Couldn't fetch Microsoft's ConPTY ($($_.Exception.Message)); Opal Terminal will use the one built into Windows." }
    # Opal Bash, the bash Opal Terminal comes with: MSYS2 packages pinned in
    # packaging/shell/packages.lock. Without it the terminal opens the other
    # shells it finds.
    Push-Location $src
    try {
        go run ./tools/fetchshell -out (Join-Path $out 'shell') | Out-Host
        if ($LASTEXITCODE -ne 0) { Write-Warning "Couldn't set up Opal Bash; Opal Terminal will open your other shells instead." }
    } finally { Pop-Location }
    return $true
}

# Creates the Start menu shortcut. Its AppUserModelID has to match the one
# Opal Terminal sets on itself, or Windows won't show its notifications.
function New-OpalShortcut([string]$lnk, [string]$target, [string]$appId) {
    if (-not ('OpalInstall.Shortcut' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;
using System.Text;

namespace OpalInstall {
    [ComImport, Guid("00021401-0000-0000-C000-000000000046")]
    class CShellLink {}

    [ComImport, InterfaceType(ComInterfaceType.InterfaceIsIUnknown), Guid("000214F9-0000-0000-C000-000000000046")]
    interface IShellLinkW {
        void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder file, int cch, IntPtr fd, uint flags);
        void GetIDList(out IntPtr pidl);
        void SetIDList(IntPtr pidl);
        void GetDescription([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder name, int cch);
        void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string name);
        void GetWorkingDirectory([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder dir, int cch);
        void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string dir);
        void GetArguments([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder args, int cch);
        void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string args);
        void GetHotkey(out short hotkey);
        void SetHotkey(short hotkey);
        void GetShowCmd(out int showCmd);
        void SetShowCmd(int showCmd);
        void GetIconLocation([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int cch, out int index);
        void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string path, int index);
        void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string path, uint reserved);
        void Resolve(IntPtr hwnd, uint flags);
        void SetPath([MarshalAs(UnmanagedType.LPWStr)] string file);
    }

    [StructLayout(LayoutKind.Sequential, Pack = 4)]
    struct PropertyKey { public Guid FormatId; public uint PropertyId; }

    // A PROPVARIANT holding a string (VT_LPWSTR), sized for 64-bit.
    [StructLayout(LayoutKind.Explicit, Size = 24)]
    struct PropVariant {
        [FieldOffset(0)] public ushort Type;
        [FieldOffset(8)] public IntPtr Value;
    }

    [ComImport, InterfaceType(ComInterfaceType.InterfaceIsIUnknown), Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99")]
    interface IPropertyStore {
        void GetCount(out uint count);
        void GetAt(uint index, out PropertyKey key);
        void GetValue(ref PropertyKey key, IntPtr value);
        void SetValue(ref PropertyKey key, ref PropVariant value);
        void Commit();
    }

    public static class Shortcut {
        public static void Create(string lnk, string target, string workDir, string description, string appId) {
            IShellLinkW link = (IShellLinkW)new CShellLink();
            link.SetPath(target);
            link.SetWorkingDirectory(workDir);
            link.SetIconLocation(target, 0);
            link.SetDescription(description);
            // PKEY_AppUserModel_ID
            PropertyKey key = new PropertyKey { FormatId = new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), PropertyId = 5 };
            PropVariant value = new PropVariant { Type = 31, Value = Marshal.StringToCoTaskMemUni(appId) };
            try {
                IPropertyStore store = (IPropertyStore)link;
                store.SetValue(ref key, ref value);
                store.Commit();
            } finally {
                Marshal.FreeCoTaskMem(value.Value);
            }
            ((IPersistFile)link).Save(lnk, true);
        }
    }
}
'@
    }
    [OpalInstall.Shortcut]::Create($lnk, $target, $env:USERPROFILE, 'A terminal with its own bash, set up with opal', $appId)
}

function Install-OpalTerminal {
    $termDir = Join-Path $env:LOCALAPPDATA 'opal\terminal'
    $termExe = Join-Path $termDir 'opal-terminal.exe'
    # Opal Terminal, or a program started from Opal Bash, holds files the
    # copy below has to replace.
    $running = Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -and $_.Path.StartsWith("$termDir\", [StringComparison]::OrdinalIgnoreCase) }
    if ($running) { throw 'Opal Terminal or its bash is running. Close it and run the installer again to update it.' }

    $stage = Join-Path ([IO.Path]::GetTempPath()) ("opal-terminal-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Force $stage | Out-Null
    try {
        $src = $stage
        if (-not ($isCheckout -and (Test-GoForTerminal) -and (Build-OpalTerminal $stage))) {
            $name = "opal-terminal_windows_$arch.zip"
            try { Get-ReleaseFile $name 'opal-terminal_SHA256SUMS.txt' "$stage.zip" }
            catch { if ("$_" -match '\b404\b') { throw "the latest release has no $name yet" } else { throw } }
            Expand-Archive "$stage.zip" -DestinationPath $stage -Force
            $src = Join-Path $stage "opal-terminal_windows_$arch"
        }
        New-Item -ItemType Directory -Force $termDir | Out-Null
        # A fresh Opal Bash, so files a newer one no longer has don't linger.
        $shellDir = Join-Path $termDir 'shell'
        if ((Test-Path (Join-Path $src 'shell')) -and (Test-Path $shellDir)) { Remove-Item $shellDir -Recurse -Force }
        Copy-Item (Join-Path $src '*') $termDir -Recurse -Force
    } finally {
        Remove-Item $stage, "$stage.zip" -Recurse -Force -ErrorAction SilentlyContinue
    }

    # The same path Opal Terminal checks for before using its own app ID.
    $programs = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'
    New-OpalShortcut (Join-Path $programs 'Opal Terminal.lnk') $termExe 'Opal.Terminal'
    Write-Host "Installed Opal Terminal to $termDir. Open it from the Start menu or with: opal terminal"
}

# A failure here leaves the opal install above in place.
if (-not ($NoTerminal -or $env:OPAL_NO_TERMINAL)) {
    try { Install-OpalTerminal }
    catch { Write-Warning "Opal Terminal wasn't installed: $($_.Exception.Message)" }
}

if (-not $NoSetup) { & $exe setup }

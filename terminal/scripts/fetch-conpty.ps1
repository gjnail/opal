# Downloads Microsoft's ConPTY (conpty.dll and OpenConsole.exe) from the
# Microsoft.Windows.Console.ConPTY NuGet package and puts them next to
# opal-terminal.exe. Opal Terminal uses them when they're there: unlike the
# ConPTY built into Windows, they pass sixel and kitty image sequences
# through. MIT licensed, published by Microsoft (github.com/microsoft/terminal).
param(
    [string]$Dest = (Join-Path $PSScriptRoot ".."),
    [string]$Version = "1.24.260710001",
    [ValidateSet("x64", "arm64", "x86")][string]$Arch = "x64"
)
$ErrorActionPreference = "Stop"
$tmp = Join-Path ([IO.Path]::GetTempPath()) "opal-conpty-$Version"
$pkg = "$tmp.zip"
if (-not (Test-Path $pkg)) {
    Invoke-WebRequest -UseBasicParsing -Uri "https://www.nuget.org/api/v2/package/Microsoft.Windows.Console.ConPTY/$Version" -OutFile $pkg
}
if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
Expand-Archive -Path $pkg -DestinationPath $tmp
Copy-Item (Join-Path $tmp "runtimes\win-$Arch\native\conpty.dll") $Dest -Force
Copy-Item (Join-Path $tmp "build\native\runtimes\$Arch\OpenConsole.exe") $Dest -Force
Remove-Item -Recurse -Force $tmp
Write-Host "conpty.dll and OpenConsole.exe ($Arch, $Version) copied to $Dest"

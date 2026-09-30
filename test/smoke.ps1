# Smoke test: load opal into PowerShell (5.1 or 7+) and check the essentials.
# Run with: pwsh -NoProfile -File test/smoke.ps1   (or powershell.exe on Windows)
$ErrorActionPreference = 'Stop'
$E = [char]27
function Fail($msg) { Write-Error "FAIL (PowerShell $($PSVersionTable.PSVersion)): $msg"; exit 1 }
function Plain($s) { $s -replace "$E\[[0-9;]*m", '' }
$env:OPAL_DATA_DIR = Join-Path ([IO.Path]::GetTempPath()) ("opal-data-" + [guid]::NewGuid()) # keep test commands out of your real history
$env:OPAL_COLOR = "truecolor"

$init = (& opal init pwsh) -join "`n"
if ($init -match '[^\x00-\x7F]') { Fail 'init must be pure ASCII (code-page safe)' }
Invoke-Expression $init
if ($env:OPAL_SHELL -ne 'pwsh') { Fail 'OPAL_SHELL not set' }
foreach ($n in 'gst', 'mkcd', 'j', 'up') {
    if ((Get-Command $n).CommandType -ne 'Function') { Fail "$n should be a function" }
}
if ((Get-Command gc).CommandType -ne 'Alias') { Fail 'built-in gc alias should be kept by default' }

# A native command failing with 3, as if typed interactively.
$now = Get-Date
Add-History -InputObject ([pscustomobject]@{ CommandLine = 'fail'; ExecutionStatus = 'Failed'; StartExecutionTime = $now.AddSeconds(-3); EndExecutionTime = $now })
if ($IsWindows -or $PSVersionTable.PSEdition -eq 'Desktop') { cmd /c exit 3; $raw = prompt } else { sh -c 'exit 3'; $raw = prompt }
$p = Plain $raw
if ($p -notmatch [regex]::Escape("$([char]0x2718) 3")) { Fail "failed status not shown: $p" }
if ($p -notmatch 'took 3s') { Fail "duration not shown: $p" }
if ($LASTEXITCODE -ne 3) { Fail 'prompt must preserve LASTEXITCODE' }
if (@(& opal history list 5) -notcontains 'fail') { Fail 'the finished command was not recorded in shared history' }
if (-not $global:__opal_tps) { Fail 'transient prompt not set up' }

$c = TabExpansion2 -inputScript 'opal theme set ' -cursorColumn 15
if (@($c.CompletionMatches.CompletionText) -notcontains 'fire') { Fail 'tab completion for opal is not registered' }
$c = TabExpansion2 -inputScript 'git chec' -cursorColumn 8
if (@($c.CompletionMatches.CompletionText) -notcontains 'checkout') { Fail 'git subcommand completion missing' }

$d = Join-Path ([IO.Path]::GetTempPath()) ("opal-" + [guid]::NewGuid())
mkcd ([IO.Path]::Combine($d, 'a', 'b'))
if ((Get-Location).Path -notlike '*b') { Fail 'mkcd' }
up 2
if ((Get-Location).Path.TrimEnd('\', '/') -ne $d.TrimEnd('\', '/')) { Fail "up 2 landed in $((Get-Location).Path)" }
"ok: PowerShell $($PSVersionTable.PSVersion)"

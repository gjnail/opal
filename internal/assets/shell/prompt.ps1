# opal: prompt hooks for PowerShell
# (The init script sets $global:__opal_record, __opal_tps, __opal_mark_c
#  and __opal_ctrl_r before this runs.)
$global:__opal_hist = -1
$global:__opal_lec = $global:LASTEXITCODE
$global:__opal_dir = ''
$global:__opal_transient = $false

function global:prompt {
    # Capture before anything else can overwrite them.
    $ok = $global:?
    $lec = $global:LASTEXITCODE
    if ($global:__opal_transient) {
        $global:__opal_transient = $false
        return $global:__opal_tps
    }

    $st = 0
    $dur = -1
    $cmd = @()
    $h = Get-History -Count 1
    if ($h -and $h.Id -ne $global:__opal_hist) {
        $global:__opal_hist = $h.Id
        $dur = [int64]($h.EndExecutionTime - $h.StartExecutionTime).TotalMilliseconds
        if (-not $ok) {
            # A native command sets LASTEXITCODE; a failing cmdlet doesn't.
            if ($lec -and $lec -ne $global:__opal_lec) { $st = $lec } else { $st = 1 }
        }
        if ($global:__opal_record) {
            # Base64, because Windows PowerShell mangles quotes in native arguments.
            $c = $h.CommandLine
            if ($c.Length -gt 8000) { $c = $c.Substring(0, 8000) }
            $cmd = @('--cmd64', [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($c)))
        }
    }

    $loc = $ExecutionContext.SessionState.Path.CurrentLocation
    $fs = $loc.Provider.Name -eq 'FileSystem'
    $cwd = if ($fs) { $loc.ProviderPath } else { $loc.Path }
    $rec = @()
    if ($fs -and $cwd -ne $global:__opal_dir) { $rec = @('--record'); $global:__opal_dir = $cwd }
    $jobs = @(Get-Job -State Running -ErrorAction SilentlyContinue).Count
    $w = 0
    try { $w = $Host.UI.RawUI.WindowSize.Width } catch { }

    # opal speaks UTF-8; make sure PowerShell decodes it that way.
    $enc = [Console]::OutputEncoding
    $swap = $enc.CodePage -ne 65001
    if ($swap) { [Console]::OutputEncoding = $global:__opal_utf8 }
    try {
        $out = & $env:OPAL_BIN prompt --shell pwsh --status $st --duration $dur --jobs $jobs --width $w --cwd $cwd @rec @cmd
    } finally {
        if ($swap) { [Console]::OutputEncoding = $enc }
    }

    $global:LASTEXITCODE = $lec
    $global:__opal_lec = $lec
    ($out -join "`n")
}

if (Get-Module -Name PSReadLine) {
    # Enter: collapse the finished line's prompt to just the prompt character
    # (transient prompt), and tell the terminal where the output starts.
    if ($global:__opal_tps -or $global:__opal_mark_c) {
        Set-PSReadLineKeyHandler -Key Enter -BriefDescription 'opal: run command' -LongDescription 'Run the line and collapse its prompt (transient prompt)' -ScriptBlock {
            param($key, $arg)
            $line = $null; $cursor = $null
            [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
            $tokens = $null; $errors = $null
            $null = [System.Management.Automation.Language.Parser]::ParseInput($line, [ref]$tokens, [ref]$errors)
            $complete = -not ($errors | Where-Object { $_.IncompleteInput })
            if ($complete -and $global:__opal_tps) {
                $global:__opal_transient = $true
                [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
            }
            [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine($key, $arg)
            if ($complete -and $global:__opal_mark_c) { [Console]::Write($global:__opal_mark_c) }
        }
    }
    # Ctrl+R: fuzzy-search the history shared by every shell you use.
    if ($global:__opal_ctrl_r) {
        Set-PSReadLineKeyHandler -Chord 'Ctrl+r' -BriefDescription 'opal: search history' -LongDescription 'Fuzzy-search the history shared by all your shells' -ScriptBlock {
            $line = $null; $cursor = $null
            [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
            $lec = $global:LASTEXITCODE
            $enc = [Console]::OutputEncoding
            $swap = $enc.CodePage -ne 65001
            if ($swap) { [Console]::OutputEncoding = $global:__opal_utf8 }
            $sel = $null; $picked = $false
            try {
                $q = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($line))
                $sel = & $env:OPAL_BIN history search --shell pwsh --query64 $q
                $picked = $LASTEXITCODE -eq 0
            } finally {
                if ($swap) { [Console]::OutputEncoding = $enc }
                $global:LASTEXITCODE = $lec
            }
            if ($picked -and $sel) {
                [Microsoft.PowerShell.PSConsoleReadLine]::RevertLine()
                [Microsoft.PowerShell.PSConsoleReadLine]::Insert((@($sel) -join "`n"))
            }
            [Microsoft.PowerShell.PSConsoleReadLine]::InvokePrompt()
        }
    }
}

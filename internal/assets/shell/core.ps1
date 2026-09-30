# opal: default settings for PowerShell (Windows PowerShell 5.1 and PowerShell 7+)
$__opal_psrl = Get-Module -Name PSReadLine
if ($__opal_psrl) {
    $__opal_v = $__opal_psrl.Version
    $env:OPAL_PSRL = $__opal_v.ToString()
    Set-PSReadLineOption -BellStyle None -HistoryNoDuplicates -HistorySearchCursorMovesToEnd
    # Up/Down search history for what's already typed; Tab shows a menu.
    Set-PSReadLineKeyHandler -Key UpArrow -Function HistorySearchBackward
    Set-PSReadLineKeyHandler -Key DownArrow -Function HistorySearchForward
    Set-PSReadLineKeyHandler -Key Tab -Function MenuComplete
    Set-PSReadLineKeyHandler -Chord 'Ctrl+Spacebar' -Function MenuComplete
    if ($__opal_v -ge [version]'2.1.0') {
        # Tab takes the grey suggestion when one is showing (like fish), and
        # completes otherwise. Right arrow still accepts; Ctrl+Space always completes.
        Set-PSReadLineKeyHandler -Key Tab -BriefDescription 'opal: accept suggestion or complete' -LongDescription 'Accept the inline suggestion if one is showing; otherwise open the completion menu' -ScriptBlock {
            param($key, $arg)
            $before = $null; $after = $null; $cursor = $null
            [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$before, [ref]$cursor)
            [Microsoft.PowerShell.PSConsoleReadLine]::AcceptSuggestion($key, $arg)
            [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$after, [ref]$cursor)
            if ($after -ceq $before) { [Microsoft.PowerShell.PSConsoleReadLine]::MenuComplete($key, $arg) }
        }
    }
    Set-PSReadLineKeyHandler -Chord 'Ctrl+d' -Function DeleteCharOrExit
    # Grey as-you-type suggestions (like zsh-autosuggestions) arrived in
    # PSReadLine 2.1; Windows PowerShell ships 2.0. Right arrow accepts one,
    # F2 (2.2+) switches to a list. Newer versions may already use plugins too.
    # PSReadLine 2.2+ refuses (throws) when output is redirected or lacks VT
    # support; that must never abort the rest of startup.
    if ($__opal_v -ge [version]'2.1.0' -and (Get-PSReadLineOption).PredictionSource -eq 'None') {
        try { Set-PSReadLineOption -PredictionSource History -ErrorAction Stop } catch { }
    }
    # Alt+S toggles sudo (gsudo, Windows sudo, or Unix sudo) at the start of the line.
    Set-PSReadLineKeyHandler -Chord 'Alt+s' -BriefDescription 'opal: toggle sudo' -ScriptBlock {
        $line = $null; $cursor = $null
        [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
        if ($line -match '^(gsudo|sudo) ') {
            [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $Matches[0].Length, '')
        } else {
            $tool = 'sudo'
            if (Get-Command gsudo -ErrorAction SilentlyContinue) { $tool = 'gsudo' }
            [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, 0, "$tool ")
        }
        [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
        [Microsoft.PowerShell.PSConsoleReadLine]::SetCursorPosition($line.Length)
    }
}

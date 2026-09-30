# opal: tab completion for PowerShell
# PowerShell has no completions for native tools, so opal supplies them:
# branches, remotes and changed files for git (and the git aliases), plus
# opal's own subcommands, package.json scripts and jump directories.
$global:__opal_complete = {
    param($wordToComplete, $commandAst, $cursorPosition)
    $words = @()
    foreach ($el in $commandAst.CommandElements) {
        if ($el.Extent.EndOffset -ge $cursorPosition) { break }
        if ($el -is [System.Management.Automation.Language.StringConstantExpressionAst]) { $words += $el.Value }
        else { $words += $el.Extent.Text }
    }
    if ($words.Count -eq 0) { return }
    $lec = $global:LASTEXITCODE
    $enc = [Console]::OutputEncoding
    $swap = $enc.CodePage -ne 65001
    if ($swap) { [Console]::OutputEncoding = $global:__opal_utf8 }
    try {
        $lines = & $env:OPAL_BIN complete --shell pwsh "--cur=$wordToComplete" @words
    } finally {
        if ($swap) { [Console]::OutputEncoding = $enc }
        $global:LASTEXITCODE = $lec
    }
    foreach ($l in @($lines)) {
        if (-not $l -or $l -eq '__files__') { return } # nothing: PowerShell falls back to paths
        $p = $l -split "`t"
        if ($p.Count -lt 3) { continue }
        [System.Management.Automation.CompletionResult]::new($p[0], $p[1], 'ParameterValue', $p[2])
    }
}
Register-ArgumentCompleter -Native -CommandName @(@@NAMES@@) -ScriptBlock $global:__opal_complete

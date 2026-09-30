package shells

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"opal/internal/assets"
	"opal/internal/config"
	"opal/internal/platform"
)

// toolCompletions loads the table of tools that generate their own
// completion scripts.
func toolCompletions() map[string]config.Variants {
	var m map[string]config.Variants
	if _, err := toml.Decode(string(assets.Completions), &m); err != nil {
		return nil
	}
	return m
}

func compExt(shell string) string {
	switch shell {
	case "pwsh":
		return ".ps1"
	case "fish":
		return ".fish"
	}
	return "." + shell
}

// cachedCompletion returns a cached completion script for tool, generating
// it if the cache is missing or older than the tool's binary. A failed
// generator leaves an empty marker so it isn't retried on every startup.
func cachedCompletion(tool, shell, command string) (string, bool) {
	exe, ok := platform.LookPath(tool)
	if !ok {
		return "", false
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return "", false
	}
	dir := filepath.Join(platform.DataDir(), "completions")
	cache := filepath.Join(dir, tool+compExt(shell))
	if fi, err := os.Stat(cache); err == nil && fi.ModTime().After(exeInfo.ModTime()) {
		if fi.Size() == 0 {
			return "", false // the generator failed for this version of the tool
		}
		if head, err := readHead(cache); err == nil && strings.HasPrefix(head, cacheHeader) {
			return cache, true
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false
	}
	args := strings.Fields(command)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args[1:]...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		os.WriteFile(cache, nil, 0o644) // remember the failure until the tool changes
		return "", false
	}
	script := string(out)
	if shell == "pwsh" {
		// These load lazily from inside a completer, so their helper functions
		// must be global to still exist when the completer runs next time.
		script = topLevelDef.ReplaceAllString(script, "$1 global:$2")
	}
	if err := os.WriteFile(cache, []byte(cacheHeader+" from: "+command+"\n"+script), 0o644); err != nil {
		return "", false
	}
	return cache, true
}

// cacheHeader marks scripts in the current cache format; others regenerate.
const cacheHeader = "# opal completion cache v2"

var topLevelDef = regexp.MustCompile(`(?m)^(function|filter)\s+([A-Za-z_][\w-]*)`)

func readHead(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, len(cacheHeader))
	n, err := f.Read(buf)
	return string(buf[:n]), err
}

// externalCompletions wires up cached completion scripts for installed tools.
// Some are big (deno's PowerShell script is half a megabyte), so they load
// lazily where the shell allows it: on the first Tab for that tool.
func (g *gen) externalCompletions() {
	if !g.o.Cfg.Completions {
		return
	}
	table := toolCompletions()
	tools := make([]string, 0, len(table))
	for t := range table {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	var code []string
	lazy := map[string]string{} // tool -> cached script
	for _, tool := range tools {
		if !g.o.Has(tool) {
			continue
		}
		v := table[tool]
		if g.shell == "pwsh" {
			if c := v.Values["pwsh_code"]; c != "" {
				code = append(code, strings.TrimSpace(c))
				continue
			}
		}
		command := v.Values[g.shell]
		if command == "" {
			continue
		}
		if path, ok := cachedCompletion(tool, g.shell, command); ok {
			lazy[tool] = path
		}
	}
	if len(code) == 0 && len(lazy) == 0 {
		return
	}
	g.section("tool completions")
	names := make([]string, 0, len(lazy))
	for t := range lazy {
		names = append(names, t)
	}
	sort.Strings(names)
	dir := filepath.Join(platform.DataDir(), "completions")

	switch g.shell {
	case "pwsh":
		// A stand-in completer loads the real script (which replaces the
		// stand-in), then re-runs completion so this first Tab already gets
		// real answers.
		if len(names) > 0 {
			g.line(`$global:__opal_compdir = %s`, psQuote(dir))
			g.line(`Register-ArgumentCompleter -Native -CommandName @(%s) -ScriptBlock {`, psList(names))
			g.line(`    param($wordToComplete, $commandAst, $cursorPosition)`)
			g.line(`    $tool = [IO.Path]::GetFileNameWithoutExtension($commandAst.CommandElements[0].Extent.Text)`)
			g.line(`    if ($global:__opal_lazy -contains $tool) { return }`)
			g.line(`    $global:__opal_lazy += @($tool)`)
			g.line(`    try { . (Join-Path $global:__opal_compdir "$tool.ps1") } catch { return }`)
			g.line(`    $full = $commandAst.Extent.StartScriptPosition.GetFullScript()`)
			g.line(`    ([System.Management.Automation.CommandCompletion]::CompleteInput($full, $cursorPosition, $null)).CompletionMatches`)
			g.line(`}`)
			g.line(`$global:__opal_lazy = @()`)
		}
	case "bash":
		// bash 4+: source on first use and return 124, which makes bash retry
		// completion with the definitions the script just installed.
		if len(names) > 0 {
			g.line(`_opal_compdir=%s`, shQuote(platform.ShellPath(dir, "bash")))
			g.line(`if (( BASH_VERSINFO[0] >= 4 )); then`)
			g.line(`  _opal_lazy_comp() { source "$_opal_compdir/$1.bash" 2>/dev/null && return 124; }`)
			g.line(`  complete -o default -F _opal_lazy_comp %s`, strings.Join(names, " "))
			g.line(`else`)
			for _, t := range names {
				g.line(`  source "$_opal_compdir/%s.bash" 2>/dev/null`, t)
			}
			g.line(`fi`)
		}
	case "fish":
		// fish already loads completions lazily from its search path.
		if len(names) > 0 {
			g.line(`contains -- %s $fish_complete_path; or set -g fish_complete_path %s $fish_complete_path`,
				fishQuote(platform.ShellPath(dir, "fish")), fishQuote(platform.ShellPath(dir, "fish")))
		}
	case "zsh":
		for _, t := range names {
			g.line("(( $+functions[compdef] )) && source %s 2>/dev/null", shQuote(platform.ShellPath(lazy[t], "zsh")))
		}
	}
	for _, c := range code {
		g.line("%s", c)
	}
}

func psList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = psQuote(n)
	}
	return strings.Join(q, ",")
}

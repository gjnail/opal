package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"opal/internal/config"
	"opal/internal/history"
	"opal/internal/platform"
	"opal/internal/plugin"
	"opal/internal/shells"
	"opal/internal/theme"
	"opal/internal/tools"
)

// Default PowerShell aliases that plugins commonly want (gc = Get-Content...).
var psBuiltinAliases = map[string]string{
	"gc": "Get-Content", "gcb": "Get-Clipboard", "gcm": "Get-Command", "gl": "Get-Location",
	"gm": "Get-Member", "gp": "Get-ItemProperty", "gi": "Get-Item", "gps": "Get-Process",
	"gsv": "Get-Service", "gv": "Get-Variable", "gu": "Get-Unique", "h": "Get-History",
	"r": "Invoke-History", "ni": "New-Item", "md": "mkdir", "sl": "Set-Location",
}

func cmdDoctor(args []string) int {
	u := newUI()
	in := platform.Detect()
	cfg, cfgErr := config.Load()
	th, thErr := theme.LoadOrDefault(cfg.Theme)
	shell := currentShell()

	row := func(k, v string) { fmt.Printf("  %s %s\n", u.muted(pad(k, 11)), v) }
	fmt.Printf("\n  %s %s  %s\n\n", u.gem(), u.gradient("opal "+Version), u.muted("doctor"))

	sys := in.OS + "/" + in.Arch
	switch {
	case in.WSL:
		sys += " · WSL"
		if in.WSLDistro != "" {
			sys += " (" + in.WSLDistro + ")"
		}
	case in.MSYS != "":
		sys += " · " + in.MSYS + " (Git Bash / MSYS2)"
	case in.Termux:
		sys += " · Termux"
	}
	if in.Container != "" {
		sys += " · container: " + in.Container
	}
	if in.SSH {
		sys += " · over SSH"
	}
	if in.Root {
		sys += " · " + u.warn("elevated")
	}
	row("system", sys)

	sh := u.warn("not initialized in this shell (run: opal setup)")
	if s := os.Getenv("OPAL_SHELL"); s != "" {
		sh = s
		if v := os.Getenv("OPAL_SHELL_VERSION"); v != "" {
			sh += " " + v
			if s == "pwsh" && strings.HasPrefix(v, "5.") {
				sh += " (Windows PowerShell)"
			}
			if s == "bash" && strings.HasPrefix(v, "3.") {
				sh += u.muted(" (no PS0: command timing off)")
			}
		}
	}
	row("shell", sh)
	if v := os.Getenv("OPAL_PSRL"); v != "" && os.Getenv("OPAL_SHELL") == "pwsh" {
		if strings.HasPrefix(v, "1.") || strings.HasPrefix(v, "2.0.") {
			row("readline", "PSReadLine "+v+"  "+u.warn("no as-you-type suggestions (needs 2.1+)"))
			row("", u.muted("fix: Install-Module PSReadLine -Scope CurrentUser -Force -SkipPublisherCheck, then open a new tab"))
		} else {
			row("readline", "PSReadLine "+v+u.muted(" · suggestions on (Tab or → accepts, F2 toggles list view)"))
		}
	}
	depth := platform.ColorDepth(cfg.Color)
	colors := map[int]string{0: "no color", 16: "16 colors", 256: "256 colors", 24: "truecolor"}[depth]
	row("terminal", fmt.Sprintf("%s · %s · icons: %s", in.Terminal, colors, platform.IconSet(cfg.Icons)))

	cfgLine := cfg.File
	switch {
	case cfgErr != nil:
		cfgLine += "  " + u.bad(cfgErr.Error())
	case !cfg.Loaded:
		cfgLine += "  " + u.muted("(not created yet: defaults in use)")
	}
	row("config", cfgLine)
	thLine := th.Name + u.muted(" · "+th.Description)
	if thErr != nil {
		thLine = u.bad(thErr.Error()) + " → " + th.Name
	}
	row("theme", thLine)
	row("clipboard", tools.ClipBackend())
	pms := tools.PackageManagers(cfg.Pkg.Prefer)
	switch len(pms) {
	case 0:
		row("packages", u.muted("none detected"))
	case 1:
		row("packages", pms[0])
	default:
		row("packages", pms[0]+u.muted(" · also: "+strings.Join(pms[1:], ", ")))
	}
	if out, err := exec.Command("git", "--version").Output(); err == nil {
		row("git", strings.TrimPrefix(strings.TrimSpace(string(out)), "git version "))
	} else {
		row("git", u.warn("not found (prompt shows branch only)"))
	}

	// Features, and whether this shell can use them.
	onOff := func(on bool, s string) string {
		if on {
			return u.ok("●") + " " + s
		}
		return u.muted("○ " + s)
	}
	sug := cfg.Suggest
	sugNote := "suggestions"
	switch shells.Normalize(shell) {
	case "pwsh":
		if v := os.Getenv("OPAL_PSRL"); strings.HasPrefix(v, "2.0.") || strings.HasPrefix(v, "1.") {
			sug, sugNote = false, "suggestions (needs PSReadLine 2.1+)"
		}
	case "bash":
		if os.Getenv("BLE_VERSION") == "" {
			sug, sugNote = false, "suggestions (bash needs ble.sh)"
		}
	}
	transient := cfg.Prompt.Transient && shells.Normalize(shell) != "bash"
	row("features", strings.Join([]string{
		onOff(sug, sugNote),
		onOff(transient, "transient prompt"),
		onOff(cfg.Prompt.SemanticMarks, "OSC 133 marks"),
	}, "  "))
	histNote := "shared history"
	if es, err := history.Load(); err == nil && cfg.History.Shared {
		histNote = fmt.Sprintf("shared history (%d commands, Ctrl+R)", len(es))
	}
	var tools []string
	for _, t := range []string{"docker", "kubectl", "helm", "gh", "rustup", "uv", "deno", "pnpm", "just", "npm", "winget"} {
		if platform.HasCommand(t) {
			tools = append(tools, t)
		}
	}
	compNote := "tool completions"
	if len(tools) > 0 {
		compNote += ": " + strings.Join(tools, ", ")
	}
	row("", strings.Join([]string{onOff(cfg.History.Shared, histNote), onOff(cfg.Completions, compNote)}, "  "))

	// Plugins, resolved for the current shell (or zsh as a stand-in).
	target := shells.Normalize(shell)
	if target == "" {
		target = "zsh"
		if runtime.GOOS == "windows" {
			target = "pwsh"
		}
	}
	res := plugin.Resolve(cfg, config.Target{Shell: target, OS: in.OS, WSL: in.WSL}, platform.HasCommand)
	fmt.Printf("\n  %s %s\n", u.accent("plugins"), u.muted("(as "+target+" sees them)"))
	defsBy := map[string][]string{}
	for _, d := range res.Defs {
		defsBy[d.Plugin] = append(defsBy[d.Plugin], d.Name)
	}
	for _, st := range res.Status {
		if !st.Active {
			fmt.Printf("    %s %s %s\n", u.muted("○"), pad(st.Name, 9), u.muted(st.Reason))
			continue
		}
		fmt.Printf("    %s %s %s\n", u.ok("●"), pad(st.Name, 9), u.muted(counts(st.Aliases, st.Functions)))
		if target == "pwsh" && !cfg.Shell.Pwsh.ClobberBuiltinAliases {
			var clash []string
			for _, n := range defsBy[st.Name] {
				if b, ok := psBuiltinAliases[strings.ToLower(n)]; ok {
					clash = append(clash, n+"→"+b)
				}
			}
			sort.Strings(clash)
			if len(clash) > 0 {
				fmt.Printf("      %s %s\n", u.warn("kept PowerShell built-ins:"), strings.Join(clash, " "))
				fmt.Printf("      %s\n", u.muted("set [shell.pwsh] clobber_builtin_aliases = true to use the plugin's instead"))
			}
		}
	}
	if len(cfg.Aliases)+len(cfg.Functions) > 0 {
		fmt.Printf("    %s %s %s\n", u.ok("●"), pad("config", 9), u.muted(counts(len(cfg.Aliases), len(cfg.Functions))))
	}
	if _, errs := plugin.Available(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Printf("    %s %s\n", u.bad("✗"), e)
		}
	}
	fmt.Println()
	return 0
}

func counts(aliases, funcs int) string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	return plural(aliases, "alias", "aliases") + ", " + plural(funcs, "function", "functions")
}

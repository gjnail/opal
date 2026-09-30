// Package cli implements the opal command line.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"opal/internal/ansi"
	"opal/internal/platform"
	"opal/internal/theme"
)

// Version is overridden at release time with -ldflags "-X opal/internal/cli.Version=...".
var Version = "0.1.0-dev"

type command struct {
	name, args, help string
	run              func(args []string) int
}

var commands []command

func init() {
	commands = []command{
		{"setup", "[--dry-run] [--remove] [shell...]", "Add opal to the startup file of every shell installed here", cmdSetup},
		{"init", "<zsh|bash|fish|pwsh>", "Print the init script for a shell (used by your rc file)", cmdInit},
		{"doctor", "", "Show what opal detected and how it adapted", cmdDoctor},
		{"theme", "list | preview [name] [--html | --svg] | set <name>", "Browse, preview and switch prompt themes", cmdTheme},
		{"plugin", "list | enable | disable | install <user/repo> | update | remove", "Manage plugins, including community ones from git", cmdPlugin},
		{"config", "[path | edit]", "Show or edit ~/.config/opal/config.toml", cmdConfig},
		{"greet", "", "Show the banner new terminals open with", cmdGreet},
		{"update", "[--release] [--from <checkout>]", "Upgrade opal (from its source checkout or the latest release)", cmdUpdate},
		{"history", "search | import | list [n]", "One history for every shell (Ctrl+R opens the picker)", cmdHistory},
		{"jump", "query <words> | list | add <dir> | remove <dir>", "Folders you visit often (used by j and ji)", cmdJump},
		{"open", "<path|url>", "Open with the default app, on any OS (WSL too)", cmdOpen},
		{"clip", "copy [text] | paste", "System clipboard; OSC 52 over SSH", cmdClip},
		{"extract", "<archive>...", "Unpack archives into their own folder", cmdExtract},
		{"ports", "[port]", "Listening TCP ports and who owns them", cmdPorts},
		{"path", "", "PATH, one per line, flagging missing and duplicate dirs", cmdPath},
		{"pkg", "<install|remove|search|update|upgrade|list|info> [pkg...]", "One package CLI for brew/apt/dnf/pacman/winget/scoop/...", cmdPkg},
		{"js", "<install|add|remove|run|test|dlx|exec> [args]", "npm/pnpm/yarn/bun, picked from the lockfile", cmdJS},
		{"venv", "create [dir] | activate-script [--shell s] [dir]", "Python virtualenvs that work in every shell", cmdVenv},
		{"git", "main-branch", "Git helpers for plugins", cmdGit},
		{"prompt", "[--shell s --status n ...]", "Render the prompt (called by the shell hooks)", cmdPrompt},
		{"complete", "--shell s --cur=<word> <words...>", "Tab completion (called by the shell hooks)", cmdComplete},
		{"version", "", "Print the version", func([]string) int { fmt.Println("opal", Version); return 0 }},
	}
}

// Run executes the command line and returns the exit code.
func Run(args []string) int {
	if len(args) == 0 {
		return help()
	}
	switch args[0] {
	case "-h", "--help", "help":
		return help()
	case "-v", "--version":
		fmt.Println("opal", Version)
		return 0
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	errorf("unknown command %q (see: opal help)", args[0])
	return 2
}

func help() int {
	u := newUI()
	fmt.Printf("\n  %s %s  %s\n\n", u.gem(), u.bold("opal "+Version), u.muted("prompt and shell tools for zsh, bash, fish and PowerShell"))
	fmt.Printf("  %s opal <command> [args]\n\n", u.muted("usage:"))
	groups := []struct {
		title string
		names []string
	}{
		{"Setup", []string{"setup", "init", "doctor", "update"}},
		{"Look & feel", []string{"theme", "plugin", "config", "greet"}},
		{"Tools that work the same on every OS", []string{"history", "jump", "open", "clip", "extract", "ports", "path", "pkg", "js", "venv"}},
	}
	for _, g := range groups {
		fmt.Printf("  %s\n", u.accent(g.title))
		for _, n := range g.names {
			for _, c := range commands {
				if c.name == n {
					fmt.Printf("    %-9s %s\n", c.name, c.help)
					if c.args != "" {
						fmt.Printf("    %-9s %s\n", "", u.muted(c.args))
					}
				}
			}
		}
		fmt.Println()
	}
	fmt.Printf("  %s %s\n\n", u.muted("config:"), filepath.Join(platform.ConfigDir(), "config.toml"))
	return 0
}

func errorf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "opal: "+format+"\n", a...)
}

// currentShell guesses which shell invoked us.
func currentShell() string {
	if s := os.Getenv("OPAL_SHELL"); s != "" {
		return s
	}
	if os.Getenv("PSModulePath") != "" && runtime.GOOS == "windows" && os.Getenv("MSYSTEM") == "" {
		return "pwsh"
	}
	sh := filepath.Base(os.Getenv("SHELL"))
	switch sh {
	case "zsh", "bash", "fish":
		return sh
	}
	return ""
}

// binPath is how shells should call this binary. A PATH entry that points at
// us (e.g. a Homebrew symlink) beats the resolved path, so upgrades keep working.
func binPath() string {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	if onPath, ok := platform.LookPath("opal"); ok {
		if abs, err := filepath.Abs(onPath); err == nil {
			a, errA := os.Stat(abs)
			b, errB := os.Stat(self)
			if errA == nil && errB == nil && os.SameFile(a, b) {
				return abs
			}
		}
	}
	return self
}

// ui styles output meant for people (help, doctor, setup).

type ui struct {
	depth int
	pal   *theme.Palette
}

func newUI() *ui {
	depth := platform.ColorDepth("auto")
	if !isTerminal() {
		depth = 0
	}
	th, _ := theme.LoadOrDefault(theme.Default)
	return &ui{depth: depth, pal: th.Resolve("dark", currentShell())}
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return true
	}
	// mintty (Git Bash) uses pipes rather than a console, but it is a terminal.
	return runtime.GOOS == "windows" && os.Getenv("TERM_PROGRAM") == "mintty"
}

func (u *ui) paint(c ansi.Color, bold bool, s string) string {
	if u.depth == 0 {
		return s
	}
	p := []string{}
	if bold {
		p = append(p, "1")
	}
	if fg := c.FG(u.depth); fg != "" {
		p = append(p, fg)
	}
	return "\x1b[" + strings.Join(p, ";") + "m" + s + "\x1b[0m"
}

func (u *ui) gem() string            { return u.paint(u.pal.Hue, true, "◆") }
func (u *ui) bold(s string) string   { return u.paint(u.pal.C("text"), true, s) }
func (u *ui) muted(s string) string  { return u.paint(u.pal.C("muted"), false, s) }
func (u *ui) accent(s string) string { return u.paint(u.pal.Hue, true, s) }
func (u *ui) ok(s string) string     { return u.paint(u.pal.C("staged"), false, s) }
func (u *ui) warn(s string) string   { return u.paint(u.pal.C("modified"), false, s) }
func (u *ui) bad(s string) string    { return u.paint(u.pal.C("error"), false, s) }

func (u *ui) gradient(s string) string {
	if u.depth == 0 {
		return s
	}
	runes := []rune(s)
	cols := ansi.Gradient(u.pal.Gradient, len(runes))
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(u.paint(cols[i], true, string(r)))
	}
	return b.String()
}

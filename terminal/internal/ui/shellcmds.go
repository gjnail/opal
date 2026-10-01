package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"opal/internal/config"
	"opal/internal/platform"
	"opal/internal/shells"
)

// shellName maps a program to the shell it is in opal's spelling (zsh,
// bash, fish, pwsh), or "" for anything else.
func shellName(cmd string) string {
	n := strings.ToLower(filepath.Base(strings.ReplaceAll(cmd, `\`, "/")))
	// A login shell's name starts with a dash.
	n = strings.TrimSuffix(strings.TrimPrefix(n, "-"), ".exe")
	switch n {
	case "zsh", "bash", "fish", "pwsh":
		return n
	case "powershell":
		return "pwsh"
	}
	return ""
}

// shellKind is the shell the pane is at: the one in the foreground, such
// as a bash started from zsh, else the one the pane started.
func (p *Pane) shellKind() string {
	p.procMu.Lock()
	name := p.procName
	p.procMu.Unlock()
	if s := shellName(name); s != "" {
		return s
	}
	return shellName(p.profile.Command)
}

// shellHas reports whether a shell would find a command. Opened from the
// Dock or a launcher, the terminal has the system's PATH without what
// shells add to it as they start, so the config's path entries and the
// usual install folders count too.
func shellHas(cfg *config.Config) func(string) bool {
	var dirs []string
	if runtime.GOOS != "windows" {
		for _, d := range cfg.Path {
			dirs = append(dirs, expandHome(d))
		}
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	}
	return func(name string) bool {
		if platform.HasCommand(name) {
			return true
		}
		for _, d := range dirs {
			if st, err := os.Stat(filepath.Join(d, name)); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return true
			}
		}
		return false
	}
}

// shellItems are the palette's entries for the aliases and functions opal
// defines in shell: the enabled plugins' and the config's own.
func shellItems(shell string) []paletteItem {
	if shell == "" {
		return nil
	}
	// A broken config still loads as the defaults.
	cfg, _ := config.Load()
	var out []paletteItem
	for _, d := range shells.Commands(shell, cfg, platform.Detect(), shellHas(cfg)) {
		// An alias is typed as what it stands for, which also works where
		// the shell spells it differently (a fish abbreviation) or isn't
		// opal's (over ssh). A function has only its name.
		kind, text, what := "alias", d.Body, d.Body
		if d.Func {
			kind, text, what = "function", d.Name, d.Desc
		}
		title := d.Name
		if what != "" {
			title += "  " + strings.ReplaceAll(what, "\n", " ⏎ ")
		}
		out = append(out, paletteItem{
			title: title,
			keys:  d.Plugin + " " + kind,
			run: func(w *Window, shift bool) {
				p := w.activePane()
				if p == nil {
					return
				}
				// The space leaves the prompt ready for arguments;
				// Shift+Enter runs it as it is.
				p.paste(text + " ")
				if shift {
					p.send([]byte{'\r'})
				}
			},
		})
	}
	return out
}

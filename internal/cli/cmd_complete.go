package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"opal/internal/complete"
	"opal/internal/config"
	"opal/internal/jump"
	"opal/internal/platform"
	"opal/internal/plugin"
	"opal/internal/shells"
	"opal/internal/theme"
	"opal/internal/tools"
)

// cmdComplete answers tab completion for the shell hooks:
//
//	opal complete --shell pwsh --cur=<partial word> <words before it...>
//
// Aliases are expanded first, so `gco <Tab>` completes like `git checkout`.
func cmdComplete(args []string) int {
	shell, cur := currentShell(), ""
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case len(words) == 0 && strings.HasPrefix(a, "--shell="):
			shell = strings.TrimPrefix(a, "--shell=")
		case len(words) == 0 && a == "--shell" && i+1 < len(args):
			shell = args[i+1]
			i++
		case len(words) == 0 && strings.HasPrefix(a, "--cur="):
			cur = strings.TrimPrefix(a, "--cur=")
		case len(words) == 0 && a == "--":
		default:
			words = append(words, a)
		}
	}
	shell = shells.Normalize(shell)
	cur = strings.Trim(cur, `'"`)
	if len(words) == 0 {
		return 0
	}
	words = expandAliases(words, shell)

	var cands []complete.Candidate
	switch filepath.Base(strings.TrimSuffix(strings.ToLower(words[0]), ".exe")) {
	case "git":
		cands = complete.Git(words[1:], cur)
	case "opal":
		cands = completeOpal(words[1:], cur)
	case "j":
		cands = completeJump(words[1:], cur, shell)
	}
	emit(shell, cands)
	return 0
}

// expandAliases replaces a leading opal alias with its body (a few levels
// deep, for aliases of aliases).
func expandAliases(words []string, shell string) []string {
	cfg, _ := config.Load()
	in := platform.Detect()
	res := plugin.Resolve(cfg, config.Target{Shell: shell, OS: in.OS, WSL: in.WSL}, platform.HasCommand)
	defs := map[string]plugin.Def{}
	for _, d := range res.Defs {
		defs[d.Name] = d
	}
	for depth := 0; depth < 4; depth++ {
		d, ok := defs[words[0]]
		if !ok || d.Func {
			break
		}
		body := strings.Fields(d.Body)
		if len(body) == 0 || body[0] == words[0] {
			break
		}
		words = append(body, words[1:]...)
	}
	return words
}

func completeOpal(args []string, cur string) []complete.Candidate {
	c := func(v, d string) complete.Candidate { return complete.Candidate{Value: v, Desc: d} }
	if len(args) == 0 {
		var out []complete.Candidate
		for _, cmd := range commands {
			if cmd.name != "prompt" && cmd.name != "complete" {
				out = append(out, c(cmd.name, cmd.help))
			}
		}
		return complete.Filter(out, cur)
	}
	shellList := []complete.Candidate{c("zsh", ""), c("bash", ""), c("fish", ""), c("pwsh", "PowerShell 5.1 and 7")}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		if len(rest) == 0 {
			return complete.Filter(shellList, cur)
		}
	case "setup":
		return complete.Filter(append([]complete.Candidate{c("--dry-run", "show what would change"), c("--remove", "undo setup")}, shellList...), cur)
	case "theme":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("list", "all themes"), c("preview", "see them rendered"), c("set", "switch theme")}, cur)
		}
		if rest[0] == "set" || rest[0] == "preview" {
			var out []complete.Candidate
			for _, n := range theme.List() {
				desc := ""
				if th, err := theme.Load(n); err == nil {
					desc = th.Description
				}
				out = append(out, c(n, desc))
			}
			if rest[0] == "preview" {
				out = append(out, c("--html", "HTML output"))
			}
			return complete.Filter(out, cur)
		}
	case "plugin":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("list", "all plugins"), c("enable", "turn one on"), c("disable", "turn one off")}, cur)
		}
		cfg, _ := config.Load()
		all, _ := plugin.Available()
		var out []complete.Candidate
		for name, p := range all {
			if (rest[0] == "enable") != cfg.HasPlugin(name) {
				out = append(out, c(name, p.Description))
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
		return complete.Filter(out, cur)
	case "config":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("path", "print the path"), c("edit", "open in your editor")}, cur)
		}
	case "jump":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("query", "best match"), c("list", "ranked dirs"), c("add", "remember a dir"), c("remove", "forget a dir")}, cur)
		}
		return complete.Files
	case "clip":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("copy", "copy text or stdin"), c("paste", "print the clipboard")}, cur)
		}
	case "open", "extract":
		return complete.Files
	case "pkg":
		if len(rest) == 0 {
			var out []complete.Candidate
			for _, v := range tools.Verbs {
				out = append(out, c(v, ""))
			}
			return complete.Filter(append(out, c("--dry-run", "print the command only")), cur)
		}
	case "js":
		if len(rest) == 0 {
			var out []complete.Candidate
			for _, v := range []string{"install", "add", "remove", "run", "test", "dlx", "exec"} {
				out = append(out, c(v, ""))
			}
			return complete.Filter(out, cur)
		}
		if rest[0] == "run" && len(rest) == 1 {
			return complete.Filter(packageScripts(), cur)
		}
	case "venv":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("create", "make a virtualenv"), c("activate-script", "print its activation script")}, cur)
		}
		return complete.Files
	case "git":
		if len(rest) == 0 {
			return complete.Filter([]complete.Candidate{c("main-branch", "print the main branch")}, cur)
		}
	}
	return nil
}

// packageScripts lists "scripts" from the nearest package.json.
func packageScripts() []complete.Candidate {
	dir, _ := os.Getwd()
	for {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pj struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(b, &pj) != nil {
				return nil
			}
			var out []complete.Candidate
			for name, cmd := range pj.Scripts {
				if len(cmd) > 60 {
					cmd = cmd[:57] + "..."
				}
				out = append(out, complete.Candidate{Value: name, Desc: cmd})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
			return out
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// completeJump offers remembered directories matching what's been typed.
// These replace the word outright, so they aren't prefix-filtered.
func completeJump(args []string, cur, shell string) []complete.Candidate {
	entries, err := jump.Ranked()
	if err != nil {
		return nil
	}
	words := append(append([]string(nil), args...), cur)
	if cur == "" {
		words = args
	}
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	var out []complete.Candidate
	for _, e := range entries {
		if strings.EqualFold(e.Path, cwd) || !jump.Match(e.Path, words) {
			continue
		}
		disp := e.Path
		if home != "" && strings.HasPrefix(strings.ToLower(disp), strings.ToLower(home)) {
			disp = "~" + disp[len(home):]
		}
		out = append(out, complete.Candidate{Value: platform.ShellPath(e.Path, shell), Desc: e.Path, Display: platform.ShellPath(disp, shell)})
		if len(out) == 20 {
			break
		}
	}
	return out
}

// emit prints candidates in the format each shell's hook expects.
func emit(shell string, cands []complete.Candidate) {
	clean := strings.NewReplacer("\t", " ", "\n", " ", "\r", "")
	for _, c := range cands {
		if c.Value == complete.FilesSentinel {
			fmt.Println(complete.FilesSentinel)
			continue
		}
		desc := clean.Replace(c.Desc)
		disp := c.Display
		if disp == "" {
			disp = c.Value
		}
		switch shell {
		case "pwsh":
			if desc == "" {
				desc = c.Value
			}
			fmt.Printf("%s\t%s\t%s\n", psArg(c.Value), disp, desc)
		case "fish":
			if desc != "" {
				fmt.Printf("%s\t%s\n", c.Value, desc)
			} else {
				fmt.Println(c.Value)
			}
		case "zsh":
			fmt.Printf("%s:%s\n", strings.ReplaceAll(strings.ReplaceAll(c.Value, `\`, `\\`), ":", `\:`), desc)
		default:
			fmt.Println(bashEscape(c.Value))
		}
	}
}

// psArg quotes a completion for PowerShell when it contains special characters.
func psArg(s string) string {
	if !strings.ContainsAny(s, " \t'\"`$&|;,(){}<>@#") {
		return s
	}
	s = strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’").Replace(s)
	return "'" + s + "'"
}

func bashEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(" \t'\"$`\\!&;()<>|*?[]{}#", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

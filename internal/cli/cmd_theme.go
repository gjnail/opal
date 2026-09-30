package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"opal/internal/ansi"
	"opal/internal/config"
	"opal/internal/git"
	"opal/internal/platform"
	"opal/internal/plugin"
	"opal/internal/prompt"
	"opal/internal/shells"
	"opal/internal/theme"
)

func cmdTheme(args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list", "ls":
		cfg, _ := config.Load()
		u := newUI()
		for _, n := range theme.List() {
			th, err := theme.Load(n)
			if err != nil {
				fmt.Printf("  %s %s\n", u.bad("✗"), err)
				continue
			}
			mark := "  "
			if n == cfg.Theme {
				mark = u.accent("● ")
			}
			fmt.Printf("  %s%s %s\n", mark, u.bold(pad(n, 10)), u.muted(th.Description))
		}
		return 0
	case "preview":
		format := ""
		var names []string
		for _, a := range args[1:] {
			switch a {
			case "--html", "--svg":
				format = a[2:]
			default:
				names = append(names, a)
			}
		}
		if len(names) == 0 {
			names = theme.List()
		}
		switch format {
		case "html":
			return previewHTML(names)
		case "svg":
			return previewSVG(names)
		}
		return previewTerminal(names)
	case "set", "use":
		if len(args) < 2 {
			errorf("usage: opal theme set <name>")
			return 2
		}
		if _, err := theme.Load(args[1]); err != nil {
			errorf("%v", err)
			return 1
		}
		if err := config.SetKey("theme", fmt.Sprintf("%q", args[1])); err != nil {
			errorf("%v", err)
			return 1
		}
		u := newUI()
		fmt.Printf("%s theme is now %s %s\n", u.ok("✓"), u.bold(args[1]), u.muted("(next prompt; syntax colors after `reload`)"))
		return 0
	}
	errorf("usage: opal theme list | preview [name] [--html | --svg] | set <name>")
	return 2
}

// sample is a staged prompt used for previews.
type sample struct {
	shell    string
	status   int
	duration int64
	jobs     int
	venv     string
}

var samples = []sample{
	{shell: "zsh", duration: 3200},
	{shell: "bash", venv: "api"},
	{shell: "fish", jobs: 1},
	{shell: "pwsh", status: 127},
}

func sampleLines(th *theme.Theme, cfg *config.Config, s sample) [][]prompt.Span {
	c := prompt.NewContext(cfg, th, s.shell)
	home := c.Info.Home
	if home == "" {
		home = string(filepath.Separator)
	}
	root := filepath.Join(home, "src", "opal")
	c.Cwd = filepath.Join(root, "internal", "prompt")
	c.Status, c.DurationMS, c.Jobs, c.Width = s.status, s.duration, s.jobs, 76
	c.FakeVenv = s.venv
	c.FakeGit = &git.Status{Root: root, Branch: "main", Ahead: 1, Staged: 2, Modified: 1, Untracked: 3}
	c.Now = time.Date(2026, 9, 29, 9, 41, 0, 0, time.Local)
	// Previews show the real host segment only when it would really appear.
	c.Info.SSH, c.Info.Container, c.Info.Root = false, "", false
	return prompt.Render(c)
}

func previewTerminal(names []string) int {
	cfg, _ := config.Load()
	u := newUI()
	depth := platform.ColorDepth(cfg.Color)
	if !isTerminal() {
		depth = 0
	}
	for _, n := range names {
		th, err := theme.Load(n)
		if err != nil {
			errorf("%v", err)
			continue
		}
		fmt.Printf("\n  %s  %s\n\n", u.bold(th.Name), u.muted(th.Description))
		for _, s := range samples {
			lines := sampleLines(th, cfg, s)
			for i, l := range lines {
				label := "      "
				if i == 0 {
					label = pad(s.shell, 6)
				}
				fmt.Printf("  %s  %s\n", u.muted(label), prompt.Encode([][]prompt.Span{l}, "raw", depth))
			}
		}
	}
	fmt.Println()
	return 0
}

// docsConfig is the configuration previews for the website and README use:
// the defaults, so the output doesn't depend on whoever generates it.
func docsConfig() *config.Config {
	cfg := config.Defaults()
	cfg.Icons = "unicode"
	return cfg
}

// previewHTML prints one <pre class="term"> block per theme, for the website.
func previewHTML(names []string) int {
	cfg := docsConfig()
	var b strings.Builder
	for _, n := range names {
		th, err := theme.Load(n)
		if err != nil {
			errorf("%v", err)
			return 1
		}
		fmt.Fprintf(&b, "<pre class=\"term\" aria-label=\"The %s theme in zsh, bash, fish and PowerShell\">", th.Name)
		for si, s := range samples {
			for i, l := range sampleLines(th, cfg, s) {
				if si > 0 || i > 0 {
					b.WriteString("\n")
				}
				label := ""
				if i == 0 {
					label = s.shell
				}
				fmt.Fprintf(&b, `<span class="term-label">%-5s</span>`, label)
				b.WriteString(prompt.HTML([][]prompt.Span{l}))
			}
		}
		b.WriteString("</pre>\n")
	}
	fmt.Print(b.String())
	return 0
}

// previewSVG draws the themes as an SVG image, for the README (GitHub
// strips colors from inline HTML). Each span is placed on a fixed cell grid
// so glyphs from fallback fonts can't shift the columns.
func previewSVG(names []string) int {
	const cw, lh, pad, labelCols = 8.4, 22.0, 18.0, 7
	cfg := docsConfig()
	type row struct {
		title, label string
		spans        []prompt.Span
	}
	var rows []row
	cols := 0
	for ti, n := range names {
		th, err := theme.Load(n)
		if err != nil {
			errorf("%v", err)
			return 1
		}
		if ti > 0 {
			rows = append(rows, row{})
		}
		rows = append(rows, row{title: th.Name})
		for _, s := range samples {
			for i, l := range sampleLines(th, cfg, s) {
				r := row{spans: l}
				if i == 0 {
					r.label = s.shell
				}
				rows = append(rows, r)
				w := 0
				for _, sp := range l {
					w += ansi.Width(sp.Text)
				}
				cols = max(cols, w)
			}
		}
	}
	width := pad*2 + float64(labelCols+cols)*cw
	height := pad*2 + float64(len(rows))*lh
	esc := func(s string) string { return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s) }
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`+"\n", width, height, width, height)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" rx="10" fill="#14131c"/>`+"\n")
	b.WriteString(`<g font-family="ui-monospace, SFMono-Regular, Menlo, Consolas, 'Cascadia Mono', monospace" font-size="14" xml:space="preserve">` + "\n")
	for i, r := range rows {
		top := pad + float64(i)*lh
		base := top + lh*0.72
		switch {
		case r.title != "":
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#e9e6f2" font-weight="700">%s</text>`+"\n", pad, base, esc(r.title))
			continue
		case r.label != "":
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="#6f6b87">%s</text>`+"\n", pad, base, esc(r.label))
		}
		col := labelCols
		for _, sp := range r.spans {
			w := ansi.Width(sp.Text)
			x := pad + float64(col)*cw
			if sp.BG.Set && w > 0 {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n", x, top+2, float64(w)*cw, lh-4, sp.BG.Hex())
			}
			if text := strings.TrimLeft(sp.Text, " "); strings.TrimSpace(text) != "" {
				// Browsers drop leading spaces in SVG text, so move the x instead.
				x += float64(len(sp.Text)-len(text)) * cw
				fill := "#e9e6f2"
				if sp.FG.Set {
					fill = sp.FG.Hex()
				}
				weight := ""
				if sp.Bold {
					weight = ` font-weight="700"`
				}
				fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s"%s>%s</text>`+"\n", x, base, fill, weight, esc(text))
			}
			col += w
		}
	}
	b.WriteString("</g>\n</svg>\n")
	fmt.Print(b.String())
	return 0
}

func cmdPlugin(args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	cfg, _ := config.Load()
	all, errs := plugin.Available()
	switch args[0] {
	case "list", "ls":
		u := newUI()
		names := make([]string, 0, len(all))
		for n := range all {
			names = append(names, n)
		}
		sort.Strings(names)
		shell := shells.Normalize(currentShell())
		if shell == "" {
			shell = "zsh"
		}
		in := platform.Detect()
		res := plugin.Resolve(cfg, config.Target{Shell: shell, OS: in.OS, WSL: in.WSL}, platform.HasCommand)
		state := map[string]plugin.Status{}
		for _, s := range res.Status {
			state[s.Name] = s
		}
		for _, n := range names {
			p := all[n]
			mark, note := u.muted("○"), ""
			if st, on := state[n]; on {
				if st.Active {
					mark = u.ok("●")
				} else {
					mark, note = u.warn("◐"), u.warn(" ("+st.Reason+")")
				}
			}
			src := ""
			if p.Source != "builtin" {
				src = u.muted(" [" + p.Source + "]")
			}
			fmt.Printf("  %s %s %s%s%s\n", mark, u.bold(pad(n, 9)), u.muted(p.Description), note, src)
		}
		for _, e := range errs {
			fmt.Printf("  %s %v\n", u.bad("✗"), e)
		}
		fmt.Printf("\n  %s enabled   %s needs a missing tool   %s available\n", u.ok("●"), u.warn("◐"), u.muted("○"))
		return 0
	case "enable", "disable":
		if len(args) < 2 {
			errorf("usage: opal plugin %s <name>...", args[0])
			return 2
		}
		list := append([]string(nil), cfg.Plugins...)
		for _, name := range args[1:] {
			if _, ok := all[name]; !ok && args[0] == "enable" {
				errorf("no plugin named %q (see: opal plugin list)", name)
				return 1
			}
			list = removeStr(list, name)
			if args[0] == "enable" {
				list = append(list, name)
			}
		}
		if err := config.SetKey("plugins", config.QuoteList(list)); err != nil {
			errorf("%v", err)
			return 1
		}
		u := newUI()
		fmt.Printf("%s plugins: %s %s\n", u.ok("✓"), strings.Join(list, " "), u.muted("(run `reload` to apply)"))
		return 0
	case "install", "add":
		if len(args) < 2 {
			errorf("usage: opal plugin install <git url | github-user/repo>")
			return 2
		}
		return pluginInstall(cfg, args[1])
	case "update", "upgrade":
		return pluginUpdate(args[1:])
	case "remove", "uninstall", "rm":
		if len(args) < 2 {
			errorf("usage: opal plugin remove <name>")
			return 2
		}
		return pluginRemove(cfg, args[1])
	}
	errorf("usage: opal plugin list | enable <name> | disable <name> | install <repo> | update [name] | remove <name>")
	return 2
}

func removeStr(list []string, s string) []string {
	out := list[:0]
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func cmdConfig(args []string) int {
	if len(args) == 0 || args[0] == "path" {
		fmt.Println(config.Path())
		return 0
	}
	if args[0] != "edit" {
		errorf("usage: opal config [path | edit]")
		return 2
	}
	p, _, err := config.WriteDefault()
	if err != nil {
		errorf("%v", err)
		return 1
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		switch {
		case runtime.GOOS == "windows":
			editor = "notepad"
		case platform.HasCommand("nano"):
			editor = "nano"
		default:
			editor = "vi"
		}
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], p)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		errorf("%s: %v", editor, err)
		return 1
	}
	return 0
}

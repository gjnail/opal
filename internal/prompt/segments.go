package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"opal/internal/ansi"
	"opal/internal/platform"
)

var segmentFuncs = map[string]func(*Context) []Span{
	"gem":      segGem,
	"shell":    segShell,
	"host":     segHost,
	"dir":      segDir,
	"git":      segGit,
	"venv":     segVenv,
	"duration": segDuration,
	"jobs":     segJobs,
	"status":   segStatus,
	"time":     segTime,
}

// gem: a symbol in the current shell's color.
func segGem(c *Context) []Span {
	return []Span{{Text: c.Sym.Gem, FG: c.Pal.Hue, Bold: true}}
}

// shell: the gem plus the shell's name (used by block themes).
func segShell(c *Context) []Span {
	name := c.Shell
	if name == "pwsh" && strings.HasPrefix(os.Getenv("OPAL_SHELL_VERSION"), "5.") {
		name = "powershell"
	}
	if name == "" {
		name = "sh"
	}
	fg := c.Pal.C("on_hue")
	return []Span{{Text: c.Sym.Gem + " " + name, FG: fg, Bold: true}}
}

// host: user@host, but only when it tells you something (SSH, containers,
// root/admin). A local, unprivileged session stays clean.
func segHost(c *Context) []Span {
	in := c.Info
	if !in.SSH && in.Container == "" && !in.Root {
		return nil
	}
	var out []Span
	if in.Container != "" && !in.SSH {
		out = append(out, Span{Text: c.Sym.Container + " ", FG: c.Pal.C("host")})
	}
	userCol := c.Pal.C("host")
	if in.Root {
		userCol = c.Pal.C("root")
	}
	out = append(out, Span{Text: in.User, FG: userCol, Bold: in.Root})
	if in.SSH || in.Container != "" {
		out = append(out, Span{Text: "@", FG: c.Pal.C("muted")}, Span{Text: in.Host, FG: c.Pal.C("host")})
	}
	return out
}

// dir: the working directory, home-relative (or repo-relative), shortened
// past max_dir_depth, and painted with the theme's gradient.
func segDir(c *Context) []Span {
	type piece struct {
		text string
		bold bool
	}
	cwd := c.Cwd
	sep := c.pathSep()
	var lead string
	var comps []string
	leadBold := false

	g := c.git()
	home := c.Info.Home
	if g != nil && g.Root != "" && !samePath(g.Root, home) && filepath.Dir(g.Root) != g.Root {
		if rel, ok := relTo(cwd, g.Root); ok {
			lead, comps, leadBold = filepath.Base(g.Root), rel, true
		}
	}
	if lead == "" {
		if rel, ok := relTo(cwd, home); ok {
			lead, comps = "~", rel
		} else {
			vol, cs := splitPath(cwd)
			comps = cs
			switch {
			case runtime.GOOS == "windows" && sep == "/" && len(vol) == 2 && vol[1] == ':':
				lead = platform.ToMSYS(vol) // C: -> /c
			default:
				lead = vol // "" on Unix means the root
			}
		}
	}

	max := c.Cfg.Prompt.MaxDirDepth
	if max <= 0 {
		max = 4
	}
	short := c.Theme.DirStyle == "short"
	var shown []piece
	if !short && len(comps) > max {
		shown = append(shown, piece{text: c.Sym.Ellipsis})
		comps = comps[len(comps)-max:]
	}
	for i, p := range comps {
		last := i == len(comps)-1
		if short && !last {
			p = abbreviate(p)
		}
		shown = append(shown, piece{text: p, bold: last})
	}

	var parts []piece
	switch {
	case lead == "":
		parts = append(parts, piece{text: sep})
		for i, p := range shown {
			if i > 0 {
				parts = append(parts, piece{text: sep})
			}
			parts = append(parts, p)
		}
	default:
		parts = append(parts, piece{text: lead, bold: leadBold || len(shown) == 0})
		for _, p := range shown {
			parts = append(parts, piece{text: sep}, p)
		}
		if len(shown) == 0 && strings.HasSuffix(lead, ":") {
			parts = append(parts, piece{text: sep}) // C:\ rather than C:
		}
	}

	// Paint: gradient across every rune, or the solid "dir" color.
	total := 0
	for _, p := range parts {
		total += len([]rune(p.text))
	}
	var colors []ansi.Color
	if len(c.Pal.Gradient) > 0 {
		n := total
		if n < 16 {
			n = 16 // short paths use only the start of the gradient
		}
		colors = ansi.Gradient(c.Pal.Gradient, n)
	}
	solid := c.Pal.C("dir")
	var out []Span
	i := 0
	for _, p := range parts {
		for _, r := range p.text {
			col := solid
			if colors != nil {
				col = colors[i]
			}
			out = append(out, Span{Text: string(r), FG: col, Bold: p.bold})
			i++
		}
	}
	return out
}

func abbreviate(p string) string {
	r := []rune(p)
	if len(r) > 1 && r[0] == '.' {
		return string(r[:2])
	}
	if len(r) > 0 {
		return string(r[:1])
	}
	return p
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func segGit(c *Context) []Span {
	g := c.git()
	if g == nil {
		return nil
	}
	p := c.Pal
	var out []Span
	if c.Theme.Style != "blocks" && c.Sym.On != "" && c.Theme.GitStyle != "compact" {
		out = append(out, Span{Text: c.Sym.On + " ", FG: p.C("muted")})
	}
	if c.Sym.Branch != "" {
		out = append(out, Span{Text: c.Sym.Branch + " ", FG: p.C("branch")})
	}
	name := g.Branch
	if g.Detached {
		name = "@" + g.Commit
	}
	out = append(out, Span{Text: name, FG: p.C("branch"), Bold: true})
	if g.State != "" {
		out = append(out, Span{Text: " " + strings.ToUpper(g.State), FG: p.C("state"), Bold: true})
	}
	if c.Theme.GitStyle == "compact" {
		if g.Dirty() {
			out = append(out, Span{Text: "*", FG: p.C("modified")})
		}
		arrows := ""
		if g.Ahead > 0 {
			arrows += c.Sym.Ahead
		}
		if g.Behind > 0 {
			arrows += c.Sym.Behind
		}
		if arrows != "" {
			out = append(out, Span{Text: " " + arrows, FG: p.C("ahead")})
		}
		return out
	}
	add := func(sym string, n int, color string) {
		if n > 0 {
			out = append(out, Span{Text: " " + sym + strconv.Itoa(n), FG: p.C(color)})
		}
	}
	add(c.Sym.Ahead, g.Ahead, "ahead")
	add(c.Sym.Behind, g.Behind, "behind")
	add(c.Sym.Conflict, g.Conflicts, "conflict")
	add(c.Sym.Staged, g.Staged, "staged")
	add(c.Sym.Modified, g.Modified, "modified")
	add(c.Sym.Untracked, g.Untracked, "untracked")
	add(c.Sym.Stash, g.Stash, "stash")
	if g.Partial {
		out = append(out, Span{Text: " " + c.Sym.Ellipsis, FG: p.C("muted")})
	}
	return out
}

func segVenv(c *Context) []Span {
	name := c.FakeVenv
	if name == "" {
		if v := os.Getenv("VIRTUAL_ENV"); v != "" {
			name = filepath.Base(v)
			if name == ".venv" || name == "venv" || name == "env" {
				name = filepath.Base(filepath.Dir(v))
			}
		} else if conda := os.Getenv("CONDA_DEFAULT_ENV"); conda != "" && conda != "base" {
			name = conda
		}
	}
	if name == "" {
		return nil
	}
	return []Span{{Text: c.Sym.Venv + " " + name, FG: c.Pal.C("venv")}}
}

func segDuration(c *Context) []Span {
	threshold := int64(c.Cfg.Prompt.DurationThreshold * 1000)
	if c.DurationMS < 0 || c.DurationMS < threshold {
		return nil
	}
	return []Span{{Text: c.Sym.Took + " " + FormatDuration(c.DurationMS), FG: c.Pal.C("duration")}}
}

func segJobs(c *Context) []Span {
	if c.Jobs <= 0 {
		return nil
	}
	return []Span{{Text: c.Sym.Jobs + " " + strconv.Itoa(c.Jobs), FG: c.Pal.C("jobs")}}
}

func segStatus(c *Context) []Span {
	if c.Status == 0 {
		return nil
	}
	out := []Span{{Text: c.Sym.Error + " " + strconv.Itoa(c.Status), FG: c.Pal.C("error"), Bold: true}}
	if m := StatusMeaning(c.Status); m != "" {
		out = append(out, Span{Text: " " + m, FG: c.Pal.C("muted")})
	}
	return out
}

func segTime(c *Context) []Span {
	if !c.Cfg.Prompt.Time && !containsStr(c.Theme.Left, "time") {
		return nil
	}
	return []Span{{Text: c.Now.Format("15:04"), FG: c.Pal.C("time")}}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// FormatDuration renders 850ms, 3.2s, 4m 12s, 1h 3m.
func FormatDuration(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		s := fmt.Sprintf("%.1f", float64(ms)/1000)
		return strings.TrimSuffix(s, ".0") + "s"
	case ms < 3_600_000:
		return fmt.Sprintf("%dm %ds", ms/60_000, (ms%60_000)/1000)
	}
	return fmt.Sprintf("%dh %dm", ms/3_600_000, (ms%3_600_000)/60_000)
}

// StatusMeaning explains well-known exit codes, including signal numbers on
// Unix and NTSTATUS codes on Windows.
func StatusMeaning(code int) string {
	switch code {
	case 126:
		return "not executable"
	case 127:
		return "not found"
	case 129:
		return "SIGHUP"
	case 130:
		return "SIGINT"
	case 131:
		return "SIGQUIT"
	case 134:
		return "SIGABRT"
	case 137:
		return "SIGKILL"
	case 139:
		return "SIGSEGV"
	case 141:
		return "SIGPIPE"
	case 143:
		return "SIGTERM"
	case -1073741510: // 0xC000013A STATUS_CONTROL_C_EXIT
		return "ctrl-c"
	case -1073741819: // 0xC0000005 STATUS_ACCESS_VIOLATION
		return "access violation"
	case -1073741515: // 0xC0000135 STATUS_DLL_NOT_FOUND
		return "dll not found"
	}
	return ""
}

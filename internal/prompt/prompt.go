// Package prompt renders the prompt: it builds styled spans from segments
// (dir, git, duration...), lays them out per the theme, and encodes them with
// the escaping each shell needs.
package prompt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"opal/internal/ansi"
	"opal/internal/config"
	"opal/internal/git"
	"opal/internal/platform"
	"opal/internal/theme"
)

// Span is a run of styled text.
type Span struct {
	Text   string
	FG, BG ansi.Color
	Bold   bool
	Seq    string // a zero-width control sequence emitted as-is (Text is ignored)
}

// Context is everything a render needs.
type Context struct {
	Shell      string // zsh | bash | fish | pwsh (drives hue and path style)
	Status     int
	DurationMS int64 // -1 when no command ran
	Jobs       int
	Width      int
	Cwd        string

	Cfg   *config.Config
	Theme *theme.Theme
	Pal   *theme.Palette
	Sym   Symbols
	Info  platform.Info
	Now   time.Time

	// For previews: pre-seeded values instead of real lookups.
	FakeGit *git.Status
	// Where last-known git counts are cached, and how to refresh them in
	// the background when git status misses its time budget.
	GitCacheDir string
	RefreshGit  func(root string)
	FakeVenv    string
	gitLoaded   bool
	gitStatus   *git.Status
}

// NewContext wires config, theme and platform together for shell.
func NewContext(cfg *config.Config, th *theme.Theme, shell string) *Context {
	c := &Context{
		Shell:      shell,
		DurationMS: -1,
		Cfg:        cfg,
		Theme:      th,
		Pal:        th.Resolve(cfg.Background, shell),
		Sym:        SymbolSet(platform.IconSet(cfg.Icons)),
		Info:       platform.Detect(),
		Now:        time.Now(),
	}
	if th.Gem != "" {
		c.Sym.Gem = th.Gem
	}
	if th.Char != "" {
		c.Sym.Char = th.Char
	}
	return c
}

func (c *Context) git() *git.Status {
	if c.FakeGit != nil {
		return c.FakeGit
	}
	if !c.gitLoaded {
		c.gitLoaded = true
		if c.Cwd != "" {
			timeout := time.Duration(c.Cfg.Prompt.GitTimeoutMS) * time.Millisecond
			if timeout <= 0 {
				timeout = 400 * time.Millisecond
			}
			c.gitStatus = git.ReadBudget(c.Cwd, timeout, c.GitCacheDir, c.RefreshGit)
		}
	}
	return c.gitStatus
}

// Render lays out the prompt as lines of spans.
func Render(c *Context) [][]Span {
	th := c.Theme
	left := c.segments(th.Left)
	right := c.segments(th.Right)
	char := c.charSpans()

	var line1 []Span
	if th.Style == "blocks" {
		line1 = c.blocks(left)
	} else {
		line1 = join(left, Span{Text: " "})
	}

	var lines [][]Span
	if th.Lines >= 2 {
		if th.Frame && th.Style != "blocks" {
			line1 = append([]Span{{Text: c.Sym.FrameTop, FG: c.Pal.C("frame")}}, line1...)
		}
		rightSpans := join(right, Span{Text: "  "})
		if len(rightSpans) > 0 {
			gap := c.Width - width(line1) - width(rightSpans) - 1
			if c.Width <= 0 || gap < 2 {
				gap = 2
			}
			line1 = append(line1, Span{Text: strings.Repeat(" ", gap)})
			line1 = append(line1, rightSpans...)
		}
		var line2 []Span
		if th.Frame && th.Style != "blocks" {
			line2 = append(line2, Span{Text: c.Sym.FrameBottom, FG: c.Pal.C("frame")})
		}
		line2 = append(line2, char...)
		lines = [][]Span{line1, line2}
	} else {
		if len(right) > 0 {
			line1 = append(line1, Span{Text: " "})
			line1 = append(line1, join(right, Span{Text: " "})...)
		}
		if len(line1) > 0 {
			line1 = append(line1, Span{Text: " "})
		}
		lines = [][]Span{append(line1, char...)}
	}
	if c.Cfg.Prompt.BlankLine {
		lines = append([][]Span{nil}, lines...)
	}
	return lines
}

// String renders and encodes for c.Shell.
func String(c *Context, depth int) string {
	return Encode(Render(c), c.Shell, depth)
}

func (c *Context) charSpans() []Span {
	col := c.Pal.Hue
	if c.Status != 0 {
		col = c.Pal.C("error")
	}
	return []Span{{Text: c.Sym.Char, FG: col, Bold: true}, {Text: " "}}
}

type segment struct {
	name  string
	spans []Span
}

func (c *Context) segments(names []string) []segment {
	var out []segment
	for _, n := range names {
		f, ok := segmentFuncs[n]
		if !ok {
			continue
		}
		if sp := f(c); len(sp) > 0 {
			out = append(out, segment{n, sp})
		}
	}
	return out
}

func join(segs []segment, sep Span) []Span {
	var out []Span
	for i, s := range segs {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, s.spans...)
	}
	return out
}

// blocks renders segments as colored pills. With a Nerd Font, powerline
// arrows connect them; otherwise adjacent backgrounds do the work.
func (c *Context) blocks(segs []segment) []Span {
	var out []Span
	text := c.Pal.C("text")
	for i, s := range segs {
		bg := c.Pal.Block(s.name)
		if s.name == "shell" {
			bg = c.Pal.Hue
		}
		if i > 0 && c.Sym.Sep != "" {
			prev := c.Pal.Block(segs[i-1].name)
			if segs[i-1].name == "shell" {
				prev = c.Pal.Hue
			}
			out = append(out, Span{Text: c.Sym.Sep, FG: prev, BG: bg})
		}
		out = append(out, Span{Text: " ", BG: bg})
		for _, sp := range s.spans {
			if !sp.FG.Set {
				sp.FG = text
			}
			sp.BG = bg
			out = append(out, sp)
		}
		out = append(out, Span{Text: " ", BG: bg})
	}
	if len(segs) > 0 && c.Sym.SepEnd != "" {
		last := segs[len(segs)-1]
		bg := c.Pal.Block(last.name)
		if last.name == "shell" {
			bg = c.Pal.Hue
		}
		out = append(out, Span{Text: c.Sym.SepEnd, FG: bg})
	}
	return out
}

func width(spans []Span) int {
	w := 0
	for _, s := range spans {
		w += ansi.Width(s.Text)
	}
	return w
}

// Cwd returns the directory to render: explicit (PowerShell passes its own
// location, which may not be a filesystem path) or the process cwd.
func Cwd(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return os.Getenv("PWD")
}

// pathSep is how the current shell spells directory separators.
func (c *Context) pathSep() string {
	if runtime.GOOS == "windows" && (c.Shell == "pwsh" || c.Shell == "") {
		return `\`
	}
	return "/"
}

// splitPath splits p into a lead (volume/root) and components.
func splitPath(p string) (vol string, comps []string) {
	vol = filepath.VolumeName(p)
	rest := p[len(vol):]
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == '/' || (runtime.GOOS == "windows" && r == '\\')
	}) {
		comps = append(comps, part)
	}
	return vol, comps
}

// relTo returns p's components below base when p is inside base.
func relTo(p, base string) ([]string, bool) {
	if base == "" {
		return nil, false
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, false
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.VolumeName(p), filepath.VolumeName(base)) {
		return nil, false
	}
	if rel == "." {
		return nil, true
	}
	_, comps := splitPath(rel)
	return comps, true
}

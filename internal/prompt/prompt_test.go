package prompt

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"opal/internal/ansi"
	"opal/internal/config"
	"opal/internal/git"
	"opal/internal/theme"
)

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(lines [][]Span) string {
	return sgr.ReplaceAllString(Encode(lines, "raw", 24), "")
}

func ctx(t *testing.T, themeName, shell string) *Context {
	t.Helper()
	th, err := theme.Load(themeName)
	if err != nil {
		t.Fatal(err)
	}
	c := NewContext(config.Defaults(), th, shell)
	c.Sym = SymbolSet("unicode")
	c.Info.Home = filepath.Join(string(filepath.Separator)+"home", "u")
	if runtime.GOOS == "windows" {
		c.Info.Home = `C:\Users\u`
	}
	c.Info.SSH, c.Info.Root, c.Info.Container = false, false, ""
	c.gitLoaded = true // no real git lookups in tests
	return c
}

func TestFireLayout(t *testing.T) {
	c := ctx(t, "fire", "zsh")
	root := filepath.Join(c.Info.Home, "src", "opal")
	c.Cwd = filepath.Join(root, "internal")
	c.FakeGit = &git.Status{Root: root, Branch: "main", Ahead: 2, Modified: 1}
	c.DurationMS = 3200
	c.Width = 60
	out := plain(Render(c))
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("fire is two lines, got %q", out)
	}
	sep := "/"
	if runtime.GOOS == "windows" {
		sep = "/" // zsh on Windows (MSYS) uses forward slashes too
	}
	for _, want := range []string{"╭─◆ opal" + sep + "internal on main ⇡2 !1", "took 3.2s"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line 1 %q missing %q", lines[0], want)
		}
	}
	if !strings.HasSuffix(lines[0], "took 3.2s") {
		t.Errorf("duration should be right-aligned: %q", lines[0])
	}
	if w := len([]rune(lines[0])); w != 59 {
		t.Errorf("right-aligned line should be width-1 = 59 cells, got %d", w)
	}
	if lines[1] != "╰─❯ " {
		t.Errorf("line 2 = %q", lines[1])
	}
}

func TestHomeAndTruncation(t *testing.T) {
	c := ctx(t, "fire", "bash")
	c.Cwd = filepath.Join(c.Info.Home, "a", "b", "c", "d", "e", "f")
	got := plain([][]Span{segDir(c)})
	if got != "~/…/c/d/e/f" {
		t.Errorf("dir = %q", got)
	}
	c.Cwd = c.Info.Home
	if got := plain([][]Span{segDir(c)}); got != "~" {
		t.Errorf("home = %q", got)
	}
}

func TestShortDirStyle(t *testing.T) {
	c := ctx(t, "boulder", "fish")
	c.Cwd = filepath.Join(c.Info.Home, "projects", ".config", "opal")
	if got := plain([][]Span{segDir(c)}); got != "~/p/.c/opal" {
		t.Errorf("short dir = %q", got)
	}
}

func TestStatusAndCharColor(t *testing.T) {
	c := ctx(t, "boulder", "zsh")
	c.Cwd = c.Info.Home
	c.Status = 127
	out := plain(Render(c))
	if !strings.Contains(out, "✘ 127 not found") {
		t.Errorf("status missing: %q", out)
	}
	errColor := c.Pal.C("error")
	if char := c.charSpans()[0]; char.FG != errColor {
		t.Error("prompt char should turn the error color after a failure")
	}
}

func TestEncodeEscaping(t *testing.T) {
	lines := [][]Span{{{Text: "100%", FG: ansi.MustHex("#7FE8DA")}}}
	z := Encode(lines, "zsh", 24)
	if !strings.Contains(z, "100%%") || !strings.HasPrefix(z, "%{\x1b[") {
		t.Errorf("zsh encoding wrong: %q", z)
	}
	b := Encode(lines, "bash", 24)
	if !strings.HasPrefix(b, "\x01\x1b[") || !strings.Contains(b, "m\x02100%") {
		t.Errorf("bash encoding wrong: %q", b)
	}
	if n := Encode(lines, "fish", 0); n != "100%" {
		t.Errorf("no-color encoding should be plain text, got %q", n)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{850: "850ms", 3000: "3s", 3240: "3.2s", 252_000: "4m 12s", 3_780_000: "1h 3m"}
	for ms, want := range cases {
		if got := FormatDuration(ms); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", ms, got, want)
		}
	}
}

package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTerminalTable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPAL_CONFIG_DIR", dir)
	cfg := `theme = "fire"

[prompt]
transient = true

[terminal]
font_family = "JetBrains Mono"
font_size = 14
cursor_style = "bar"
scrollback = 5000

[terminal.colors]
background = "#101010"
palette = ["#000000", "#ff0000"]

[[terminal.profiles]]
name = "work"
command = "ssh"
args = ["box"]
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load()
	if len(c.Warnings) > 0 {
		t.Fatalf("warnings: %v", c.Warnings)
	}
	if len(c.FontFamily) != 1 || c.FontFamily[0] != "JetBrains Mono" || c.FontSize != 14 {
		t.Fatalf("font: %v %v", c.FontFamily, c.FontSize)
	}
	if c.CursorStyle != "bar" || c.Scrollback != 5000 || !c.CursorBlink {
		t.Fatalf("cursor/scrollback: %+v", c)
	}
	p := c.Palette()
	if p.Background.Hex() != "#101010" || p.ANSI[1].Hex() != "#ff0000" {
		t.Fatalf("overrides not applied: bg %s red %s", p.Background.Hex(), p.ANSI[1].Hex())
	}
	if len(c.Profiles) != 1 || c.Profiles[0].Args[0] != "box" {
		t.Fatalf("profiles: %+v", c.Profiles)
	}
}

func TestThemePaletteIsReadable(t *testing.T) {
	t.Setenv("OPAL_CONFIG_DIR", t.TempDir())
	for _, bg := range []string{"dark", "light"} {
		c := Defaults()
		c.Theme, c.Background = "fire", bg
		p := c.Palette()
		if bg == "dark" && p.Background.Luma() > 0.2 {
			t.Errorf("dark background too bright: %s", p.Background.Hex())
		}
		if bg == "light" && p.Background.Luma() < 0.8 {
			t.Errorf("light background too dark: %s", p.Background.Hex())
		}
		if d := p.Foreground.Luma() - p.Background.Luma(); d*d < 0.25 {
			t.Errorf("%s: foreground %s too close to background %s", bg, p.Foreground.Hex(), p.Background.Hex())
		}
	}
}

func TestStringList(t *testing.T) {
	if got := splitCommand(`"C:\Program Files\x.exe" -a "b c"`); len(got) != 3 || got[2] != "b c" {
		t.Fatalf("splitCommand: %q", got)
	}
}

func TestDetectProfiles(t *testing.T) {
	ps := DetectProfiles()
	if len(ps) == 0 {
		t.Fatal("no shells found")
	}
	for _, p := range ps {
		t.Logf("%s: %s %v", p.Name, p.Command, p.Args)
	}
}

func TestBundledBash(t *testing.T) {
	dir := t.TempDir()
	if _, ok := bundledBash(dir); ok {
		t.Fatal("found a bundled bash in an empty folder")
	}
	bin := filepath.Join(dir, "shell", "usr", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "bash.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p, ok := bundledBash(dir)
	if !ok || p.Name != OpalBashName || p.Command != filepath.Join(bin, "bash.exe") {
		t.Fatalf("got %+v, %v", p, ok)
	}
	// Its own startup file, never the user's ~/.bashrc.
	if got := strings.Join(p.Args, " "); got != "--noprofile --rcfile /etc/opal/bashrc -i" {
		t.Fatalf("args: %s", got)
	}
}

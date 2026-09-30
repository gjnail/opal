// Package theme loads prompt themes (built in, or TOML files in
// ~/.config/opal/themes) and resolves them into concrete colors.
package theme

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"opal/internal/ansi"
	"opal/internal/assets"
	"opal/internal/platform"
)

// Theme mirrors a theme TOML file.
type Theme struct {
	Name        string            `toml:"name"`
	Description string            `toml:"description"`
	Style       string            `toml:"style"`     // "line" or "blocks"
	Lines       int               `toml:"lines"`     // 1 or 2
	Frame       bool              `toml:"frame"`     // ╭─ / ╰─ on two-line prompts
	DirStyle    string            `toml:"dir_style"` // "full" or "short"
	GitStyle    string            `toml:"git_style"` // "full" or "compact"
	Left        []string          `toml:"left"`
	Right       []string          `toml:"right"`
	Gradient    []string          `toml:"gradient"`
	Char        string            `toml:"char"`
	Gem         string            `toml:"gem"`
	Colors      map[string]string `toml:"colors"`
	Hues        map[string]string `toml:"hues"`
	Blocks      map[string]string `toml:"blocks"`
	Syntax      map[string]string `toml:"syntax"`
	Light       *Variant          `toml:"light"`

	Source string `toml:"-"`
}

// Variant overrides colors for light backgrounds.
type Variant struct {
	Gradient []string          `toml:"gradient"`
	Colors   map[string]string `toml:"colors"`
	Hues     map[string]string `toml:"hues"`
	Blocks   map[string]string `toml:"blocks"`
}

// Default is used when the configured theme cannot be found.
const Default = "fire"

// Load finds a theme by name: user themes first, then built-ins.
func Load(name string) (*Theme, error) {
	if name == "" {
		name = Default
	}
	user := filepath.Join(platform.ConfigDir(), "themes", name+".toml")
	if b, err := os.ReadFile(user); err == nil {
		return parse(b, user)
	}
	if b, err := assets.Themes.ReadFile("themes/" + name + ".toml"); err == nil {
		return parse(b, "builtin")
	}
	return nil, fmt.Errorf("theme %q not found (try: opal theme list)", name)
}

// LoadOrDefault never fails: an unknown theme falls back to fire.
func LoadOrDefault(name string) (*Theme, error) {
	t, err := Load(name)
	if err == nil {
		return t, nil
	}
	d, derr := Load(Default)
	if derr != nil {
		panic(derr) // the built-in default must parse
	}
	return d, err
}

func parse(b []byte, source string) (*Theme, error) {
	t := &Theme{Style: "line", Lines: 1, DirStyle: "full", GitStyle: "full"}
	if _, err := toml.Decode(string(b), t); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	t.Source = source
	return t, nil
}

// List returns every available theme name, sorted.
func List() []string {
	seen := map[string]bool{}
	entries, _ := fs.ReadDir(assets.Themes, "themes")
	for _, e := range entries {
		seen[strings.TrimSuffix(e.Name(), ".toml")] = true
	}
	if user, err := os.ReadDir(filepath.Join(platform.ConfigDir(), "themes")); err == nil {
		for _, e := range user {
			if strings.HasSuffix(e.Name(), ".toml") {
				seen[strings.TrimSuffix(e.Name(), ".toml")] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Palette is a theme resolved for one background and one shell.
type Palette struct {
	colors   map[string]ansi.Color
	blocks   map[string]ansi.Color
	Hue      ansi.Color
	Gradient []ansi.Color
}

// Resolve picks colors for the background ("dark" or "light") and shell.
func (t *Theme) Resolve(background, shell string) *Palette {
	colors, hues, blocks, grad := t.Colors, t.Hues, t.Blocks, t.Gradient
	if background == "light" && t.Light != nil {
		colors = merge(colors, t.Light.Colors)
		hues = merge(hues, t.Light.Hues)
		blocks = merge(blocks, t.Light.Blocks)
		if len(t.Light.Gradient) > 0 {
			grad = t.Light.Gradient
		}
	}
	p := &Palette{colors: map[string]ansi.Color{}, blocks: map[string]ansi.Color{}}
	for k, v := range colors {
		if c, ok := ansi.Hex(v); ok {
			p.colors[k] = c
		}
	}
	for k, v := range blocks {
		if c, ok := ansi.Hex(v); ok {
			p.blocks[k] = c
		}
	}
	hue, ok := hues[shell]
	if !ok {
		hue = hues["default"]
	}
	p.Hue, ok = ansi.Hex(hue)
	if !ok {
		p.Hue = p.C("text")
	}
	for _, g := range grad {
		if c, ok := ansi.Hex(g); ok {
			p.Gradient = append(p.Gradient, c)
		}
	}
	return p
}

// C looks up a named color ("hue", a palette name, or a hex literal).
func (p *Palette) C(name string) ansi.Color {
	if name == "hue" {
		return p.Hue
	}
	if c, ok := p.colors[name]; ok {
		return c
	}
	if c, ok := ansi.Hex(name); ok {
		return c
	}
	if name != "text" {
		return p.C("text")
	}
	return ansi.Color{}
}

// Block returns the background for a segment in "blocks" style.
func (p *Palette) Block(segment string) ansi.Color {
	if c, ok := p.blocks[segment]; ok {
		return c
	}
	return p.blocks["default"]
}

// SyntaxColors resolves the [syntax] table for shells with built-in
// highlighting.
func (t *Theme) SyntaxColors(p *Palette) map[string]ansi.Color {
	out := map[string]ansi.Color{}
	for k, v := range t.Syntax {
		out[k] = p.C(v)
	}
	return out
}

func merge(base, over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

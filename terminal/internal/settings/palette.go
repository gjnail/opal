package settings

import (
	"opal/internal/ansi"
	"opal/internal/theme"
	"opal/terminal/internal/vt"
)

// Palette builds the terminal's colors from an Opal prompt theme, so a
// theme styles both the prompt and everything around it. Themes name
// colors by meaning (error, staged, branch...), which map naturally onto
// the ANSI slots programs use for the same meanings.
func (c *Config) Palette() vt.Palette {
	th, _ := theme.LoadOrDefault(c.Theme)
	light := c.Background == "light"
	bgName := "dark"
	if light {
		bgName = "light"
	}
	tp := th.Resolve(bgName, "default")
	get := func(name string) vt.RGB { return rgb(tp.C(name)) }

	black := vt.RGB{}
	white := vt.RGB{R: 255, G: 255, B: 255}
	text, muted, frame := get("text"), get("muted"), get("frame")

	var p vt.Palette
	base := [6]vt.RGB{get("error"), get("staged"), get("branch"), get("ahead"), get("stash"), get("venv")}
	if !light {
		p.Background = frame.Mix(black, 0.8)
		p.Foreground = text
		p.ANSI[0] = frame.Mix(black, 0.45)
		p.ANSI[7] = text.Mix(muted, 0.35)
		p.ANSI[8] = muted
		p.ANSI[15] = text.Mix(white, 0.3)
		for i, c := range base {
			p.ANSI[1+i] = c
			p.ANSI[9+i] = c.Mix(white, 0.25)
		}
	} else {
		p.Background = frame.Mix(white, 0.94)
		p.Foreground = text
		p.ANSI[0] = text
		p.ANSI[7] = muted.Mix(p.Background, 0.55)
		p.ANSI[8] = muted
		p.ANSI[15] = p.Background.Mix(white, 0.5)
		for i, c := range base {
			p.ANSI[1+i] = c
			p.ANSI[9+i] = c.Mix(black, 0.15)
		}
	}
	vt.FillXterm256(&p.ANSI)

	accent := text
	if len(tp.Gradient) > 0 {
		accent = rgb(tp.Gradient[0])
	}
	p.Cursor = accent
	p.CursorText = p.Background
	sel := accent
	if len(tp.Gradient) > 2 {
		sel = rgb(tp.Gradient[len(tp.Gradient)/2])
	}
	if light {
		p.SelectionBG = sel.Mix(p.Background, 0.7)
	} else {
		p.SelectionBG = sel.Mix(p.Background, 0.65)
	}

	c.applyColorOverrides(&p)
	return p
}

// Accent is the theme's lead color, used by the UI chrome.
func (c *Config) Accent() vt.RGB {
	th, _ := theme.LoadOrDefault(c.Theme)
	tp := th.Resolve(c.Background, "default")
	if len(tp.Gradient) > 0 {
		return rgb(tp.Gradient[0])
	}
	return rgb(tp.C("text"))
}

// Gradient is the theme's gradient, for the tab bar's active indicator.
func (c *Config) Gradient() []vt.RGB {
	th, _ := theme.LoadOrDefault(c.Theme)
	tp := th.Resolve(c.Background, "default")
	var out []vt.RGB
	for _, g := range tp.Gradient {
		out = append(out, rgb(g))
	}
	return out
}

func rgb(c ansi.Color) vt.RGB { return vt.RGB{R: c.R, G: c.G, B: c.B} }

func (c *Config) applyColorOverrides(p *vt.Palette) {
	set := func(dst *vt.RGB, spec, key string) {
		if spec == "" {
			return
		}
		if v, ok := vt.ParseColorSpec(spec); ok {
			*dst = v
		} else {
			c.warnf("colors.%s: can't read %q", key, spec)
		}
	}
	col := c.Colors
	set(&p.Foreground, col.Foreground, "foreground")
	set(&p.Background, col.Background, "background")
	set(&p.Cursor, col.Cursor, "cursor")
	set(&p.CursorText, col.CursorText, "cursor_text")
	set(&p.SelectionBG, col.Selection, "selection")
	for i, s := range col.Palette {
		if i >= 16 {
			break
		}
		set(&p.ANSI[i], s, "palette")
	}
}

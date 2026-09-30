package render

import (
	"math"

	"opal/terminal/internal/vt"
)

// resolve turns a cell color into RGB, using def for the default color.
func (r *Renderer) resolve(c vt.Color, def vt.RGB) vt.RGB {
	if i, ok := c.Index(); ok {
		return r.pal.ANSI[i]
	}
	if rgb, ok := c.RGB(); ok {
		return rgb
	}
	return def
}

// colors works out the foreground and background a cell is drawn with.
// defBG reports that the background is the terminal's default, which the
// caller may leave transparent.
func (r *Renderer) colors(c vt.Cell, reverse, selected, cursor bool) (fg, bg vt.RGB, defBG bool) {
	fgc := c.Fg
	if r.opts.BoldIsBright && c.A&vt.AttrBold != 0 {
		if i, ok := fgc.Index(); ok && i < 8 {
			fgc = vt.Indexed(i + 8)
		}
	}
	defFG, defBGc := r.pal.Foreground, r.pal.Background
	fg = r.resolve(fgc, defFG)
	bg = r.resolve(c.Bg, defBGc)
	defBG = c.Bg.IsDefault()

	inverse := c.A&vt.AttrInverse != 0
	if inverse != reverse {
		fg, bg = bg, fg
		defBG = false
	}
	if c.A&vt.AttrDim != 0 {
		fg = fg.Mix(bg, 0.45)
	}
	if selected {
		bg = r.pal.SelectionBG
		defBG = false
		if r.pal.HasSelectionFG {
			fg = r.pal.SelectionFG
		}
	}
	if cursor {
		bg = r.pal.Cursor
		fg = r.pal.CursorText
		defBG = false
		if contrast(fg, bg) < 2 {
			fg = bestText(bg)
		}
	}
	if r.opts.MinContrast > 1 && c.A&vt.AttrDim == 0 {
		fg = ensureContrast(fg, bg, r.opts.MinContrast)
	}
	return fg, bg, defBG
}

// luminance is WCAG relative luminance.
func luminance(c vt.RGB) float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func contrast(a, b vt.RGB) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func bestText(bg vt.RGB) vt.RGB {
	if luminance(bg) > 0.4 {
		return vt.RGB{}
	}
	return vt.RGB{R: 255, G: 255, B: 255}
}

// ensureContrast moves fg toward black or white until it reaches the
// target contrast against bg, keeping its hue as long as possible.
func ensureContrast(fg, bg vt.RGB, target float64) vt.RGB {
	if contrast(fg, bg) >= target {
		return fg
	}
	toward := bestText(bg)
	lo, hi := 0.0, 1.0
	for i := 0; i < 12; i++ {
		mid := (lo + hi) / 2
		if contrast(fg.Mix(toward, mid), bg) >= target {
			hi = mid
		} else {
			lo = mid
		}
	}
	return fg.Mix(toward, hi)
}

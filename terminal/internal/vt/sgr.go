package vt

import (
	"fmt"
	"strings"
)

// sgr applies Select Graphic Rendition.
func (t *Terminal) sgr(p *params) {
	pn := &t.cur.pen
	if p.n == 0 {
		*pn = pen{attrs: pn.attrs & AttrProtected}
		return
	}
	for i := 0; i < p.n; i++ {
		v := p.get(i, 0)
		switch {
		case v == 0:
			*pn = pen{attrs: pn.attrs & AttrProtected}
		case v == 1:
			pn.attrs |= AttrBold
		case v == 2:
			pn.attrs |= AttrDim
		case v == 3:
			pn.attrs |= AttrItalic
		case v == 4:
			style := UnderlineSingle
			if i+1 < p.n && p.sub[i+1] {
				style = clamp(p.get(i+1, 1), 0, UnderlineDashed)
			}
			pn.attrs = pn.attrs.withUnderline(style)
		case v == 5:
			pn.attrs |= AttrBlink
		case v == 6:
			pn.attrs |= AttrRapidBlink
		case v == 7:
			pn.attrs |= AttrInverse
		case v == 8:
			pn.attrs |= AttrHidden
		case v == 9:
			pn.attrs |= AttrStrike
		case v == 21:
			pn.attrs = pn.attrs.withUnderline(UnderlineDouble)
		case v == 22:
			pn.attrs &^= AttrBold | AttrDim
		case v == 23:
			pn.attrs &^= AttrItalic
		case v == 24:
			pn.attrs = pn.attrs.withUnderline(UnderlineNone)
		case v == 25:
			pn.attrs &^= AttrBlink | AttrRapidBlink
		case v == 27:
			pn.attrs &^= AttrInverse
		case v == 28:
			pn.attrs &^= AttrHidden
		case v == 29:
			pn.attrs &^= AttrStrike
		case v >= 30 && v <= 37:
			pn.fg = Indexed(uint8(v - 30))
		case v == 38:
			if c, next, ok := extColor(p, i); ok {
				pn.fg = c
				i = next
			} else {
				i = next
			}
		case v == 39:
			pn.fg = DefaultColor
		case v >= 40 && v <= 47:
			pn.bg = Indexed(uint8(v - 40))
		case v == 48:
			if c, next, ok := extColor(p, i); ok {
				pn.bg = c
				i = next
			} else {
				i = next
			}
		case v == 49:
			pn.bg = DefaultColor
		case v == 53:
			pn.attrs |= AttrOverline
		case v == 55:
			pn.attrs &^= AttrOverline
		case v == 58:
			if c, next, ok := extColor(p, i); ok {
				pn.ulColor = c
				i = next
			} else {
				i = next
			}
		case v == 59:
			pn.ulColor = DefaultColor
		case v >= 90 && v <= 97:
			pn.fg = Indexed(uint8(v - 90 + 8))
		case v >= 100 && v <= 107:
			pn.bg = Indexed(uint8(v - 100 + 8))
		}
		// Skip sub-parameters we didn't consume.
		for i+1 < p.n && p.sub[i+1] {
			i++
		}
	}
}

// extColor parses the color after 38/48/58 at index i, in either the
// colon form (38:2::r:g:b, 38:5:n) or the legacy semicolon form
// (38;2;r;g;b, 38;5;n). next is the index of the last parameter consumed.
func extColor(p *params, i int) (c Color, next int, ok bool) {
	next = i
	if i+1 < p.n && p.sub[i+1] {
		var subs []int
		j := i + 1
		for ; j < p.n && p.sub[j]; j++ {
			subs = append(subs, p.get(j, 0))
		}
		next = j - 1
		if len(subs) == 0 {
			return 0, next, false
		}
		switch subs[0] {
		case 5:
			if len(subs) >= 2 {
				return Indexed(uint8(clamp(subs[1], 0, 255))), next, true
			}
		case 2:
			// 38:2:<colorspace>:r:g:b, or the common 38:2:r:g:b.
			var r, g, b int
			switch {
			case len(subs) >= 5:
				r, g, b = subs[2], subs[3], subs[4]
			case len(subs) == 4:
				r, g, b = subs[1], subs[2], subs[3]
			default:
				return 0, next, false
			}
			return RGBColor(uint8(clamp(r, 0, 255)), uint8(clamp(g, 0, 255)), uint8(clamp(b, 0, 255))), next, true
		}
		return 0, next, false
	}
	if i+1 >= p.n {
		return 0, next, false
	}
	switch p.get(i+1, 0) {
	case 5:
		if i+2 < p.n {
			return Indexed(uint8(clamp(p.get(i+2, 0), 0, 255))), i + 2, true
		}
		return 0, p.n - 1, false
	case 2:
		if i+4 < p.n {
			r, g, b := p.get(i+2, 0), p.get(i+3, 0), p.get(i+4, 0)
			return RGBColor(uint8(clamp(r, 0, 255)), uint8(clamp(g, 0, 255)), uint8(clamp(b, 0, 255))), i + 4, true
		}
		return 0, p.n - 1, false
	}
	return 0, i + 1, false
}

// sgrString renders the pen as SGR parameters, for DECRQSS.
func (p pen) sgrString() string {
	parts := []string{"0"}
	a := p.attrs
	add := func(s string) { parts = append(parts, s) }
	if a&AttrBold != 0 {
		add("1")
	}
	if a&AttrDim != 0 {
		add("2")
	}
	if a&AttrItalic != 0 {
		add("3")
	}
	switch a.Underline() {
	case UnderlineNone:
	case UnderlineSingle:
		add("4")
	default:
		add(fmt.Sprintf("4:%d", a.Underline()))
	}
	if a&AttrBlink != 0 {
		add("5")
	}
	if a&AttrRapidBlink != 0 {
		add("6")
	}
	if a&AttrInverse != 0 {
		add("7")
	}
	if a&AttrHidden != 0 {
		add("8")
	}
	if a&AttrStrike != 0 {
		add("9")
	}
	if a&AttrOverline != 0 {
		add("53")
	}
	color := func(c Color, base, ext int) {
		if i, ok := c.Index(); ok {
			switch {
			case base != 0 && i < 8:
				add(fmt.Sprint(base + int(i)))
			case base != 0 && i < 16:
				add(fmt.Sprint(base + 60 + int(i) - 8))
			default:
				add(fmt.Sprintf("%d:5:%d", ext, i))
			}
		} else if rgb, ok := c.RGB(); ok {
			add(fmt.Sprintf("%d:2::%d:%d:%d", ext, rgb.R, rgb.G, rgb.B))
		}
	}
	color(p.fg, 30, 38)
	color(p.bg, 40, 48)
	color(p.ulColor, 0, 58)
	return strings.Join(parts, ";")
}

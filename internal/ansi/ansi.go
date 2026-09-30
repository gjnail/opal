// Package ansi handles colors: hex parsing, OKLab gradients, and downsampling
// from truecolor to 256 and 16 colors.
package ansi

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Color is an sRGB color. The zero value means "unset / terminal default".
type Color struct {
	R, G, B uint8
	Set     bool
}

// Hex parses "#RRGGBB" or "#RGB".
func Hex(s string) (Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return Color{}, false
	}
	return Color{uint8(v >> 16), uint8(v >> 8), uint8(v), true}, true
}

// MustHex is Hex for known-good literals.
func MustHex(s string) Color {
	c, _ := Hex(s)
	return c
}

// Hex returns the color as "#RRGGBB".
func (c Color) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// FG returns SGR parameters that set c as the foreground at the given depth.
func (c Color) FG(depth int) string { return c.sgr(depth, false) }

// BG returns SGR parameters that set c as the background at the given depth.
func (c Color) BG(depth int) string { return c.sgr(depth, true) }

func (c Color) sgr(depth int, bg bool) string {
	if !c.Set || depth == 0 {
		return ""
	}
	base := 38
	if bg {
		base = 48
	}
	switch depth {
	case 24:
		return fmt.Sprintf("%d;2;%d;%d;%d", base, c.R, c.G, c.B)
	case 256:
		return fmt.Sprintf("%d;5;%d", base, to256(c))
	}
	n := to16(c)
	off := 30
	if bg {
		off = 40
	}
	if n >= 8 {
		return strconv.Itoa(off + 60 + n - 8)
	}
	return strconv.Itoa(off + n)
}

// Blending in OKLab keeps gradients between pastel colors from going gray in
// the middle, which plain RGB interpolation does.

type lab struct{ L, A, B float64 }

func toLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func fromLinear(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func toLab(c Color) lab {
	r, g, b := toLinear(float64(c.R)/255), toLinear(float64(c.G)/255), toLinear(float64(c.B)/255)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return lab{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func fromLab(o lab) Color {
	l := o.L + 0.3963377774*o.A + 0.2158037573*o.B
	m := o.L - 0.1055613458*o.A - 0.0638541728*o.B
	s := o.L - 0.0894841775*o.A - 1.2914855480*o.B
	l, m, s = l*l*l, m*m*m, s*s*s
	r := 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	b := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return Color{clamp8(fromLinear(r)), clamp8(fromLinear(g)), clamp8(fromLinear(b)), true}
}

func clamp8(v float64) uint8 {
	v = math.Round(v * 255)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// Mix blends a→b in OKLab; t in [0,1].
func Mix(a, b Color, t float64) Color {
	x, y := toLab(a), toLab(b)
	return fromLab(lab{x.L + (y.L-x.L)*t, x.A + (y.A-x.A)*t, x.B + (y.B-x.B)*t})
}

// Gradient spreads n colors evenly across the stops.
func Gradient(stops []Color, n int) []Color {
	out := make([]Color, n)
	if len(stops) == 0 || n == 0 {
		return out
	}
	if len(stops) == 1 || n == 1 {
		for i := range out {
			out[i] = stops[0]
		}
		return out
	}
	for i := range out {
		pos := float64(i) / float64(n-1) * float64(len(stops)-1)
		k := int(pos)
		if k >= len(stops)-1 {
			k = len(stops) - 2
		}
		out[i] = Mix(stops[k], stops[k+1], pos-float64(k))
	}
	return out
}

var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

func to256(c Color) int {
	idx := func(v uint8) int {
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		}
		return (int(v) - 35) / 40
	}
	ri, gi, bi := idx(c.R), idx(c.G), idx(c.B)
	cube := 16 + 36*ri + 6*gi + bi
	avg := (int(c.R) + int(c.G) + int(c.B)) / 3
	gi2 := 23
	if avg < 238 {
		gi2 = (avg - 3) / 10
		if gi2 < 0 {
			gi2 = 0
		}
	}
	gv := 8 + 10*gi2
	if dist(c, cubeLevels[ri], cubeLevels[gi], cubeLevels[bi]) <= dist(c, gv, gv, gv) {
		return cube
	}
	return 232 + gi2
}

func dist(c Color, r, g, b int) int {
	dr, dg, db := int(c.R)-r, int(c.G)-g, int(c.B)-b
	return dr*dr*3 + dg*dg*4 + db*db*2
}

// to16 maps by hue rather than RGB distance. Nearest-RGB turns most pastels
// into white; going by hue keeps pink as magenta and teal as cyan.
func to16(c Color) int {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l, d := (mx+mn)/2, mx-mn
	if d < 0.12 {
		switch {
		case l < 0.2:
			return 0
		case l < 0.5:
			return 8
		case l < 0.85:
			return 7
		}
		return 15
	}
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	var n int
	switch {
	case h < 30 || h >= 330:
		n = 1 // red
	case h < 90:
		n = 3 // yellow
	case h < 150:
		n = 2 // green
	case h < 210:
		n = 6 // cyan
	case h < 270:
		n = 4 // blue
	default:
		n = 5 // magenta
	}
	if l > 0.6 {
		n += 8
	}
	return n
}

// Width is the number of terminal cells s occupies (no escape sequences).
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += RuneWidth(r)
	}
	return w
}

// RuneWidth approximates wcwidth: 0 for combining marks, 2 for East Asian
// wide characters and emoji, 1 otherwise.
func RuneWidth(r rune) int {
	switch {
	case r < 32, r >= 0x7f && r < 0xa0:
		return 0
	case r == 0x200b, r == 0x200c, r == 0x200d, r >= 0xfe00 && r <= 0xfe0f:
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
		return 0
	}
	for _, rg := range wide {
		if r >= rg[0] && r <= rg[1] {
			return 2
		}
	}
	return 1
}

var wide = [][2]rune{
	{0x1100, 0x115f}, {0x2e80, 0x303e}, {0x3041, 0x33ff}, {0x3400, 0x4dbf},
	{0x4e00, 0x9fff}, {0xa000, 0xa4cf}, {0xac00, 0xd7a3}, {0xf900, 0xfaff},
	{0xfe30, 0xfe4f}, {0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x1f300, 0x1f64f},
	{0x1f900, 0x1f9ff}, {0x20000, 0x3fffd},
}

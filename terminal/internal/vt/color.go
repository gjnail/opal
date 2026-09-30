package vt

import (
	"fmt"
	"strconv"
	"strings"
)

// Color is a cell color packed into 32 bits. The top byte says what kind of
// color it is: the terminal default, an index into the 256-color palette, or
// a direct RGB value.
type Color uint32

const (
	colorKindDefault Color = 0
	colorKindIndexed Color = 1 << 24
	colorKindRGB     Color = 2 << 24
	colorKindMask    Color = 0xff << 24
)

// DefaultColor means "whatever the terminal's default foreground or
// background is", which the renderer resolves depending on where it's used.
const DefaultColor Color = 0

// Indexed returns palette color i.
func Indexed(i uint8) Color { return colorKindIndexed | Color(i) }

// RGBColor returns a direct color.
func RGBColor(r, g, b uint8) Color {
	return colorKindRGB | Color(r)<<16 | Color(g)<<8 | Color(b)
}

// IsDefault reports whether c is the terminal default color.
func (c Color) IsDefault() bool { return c&colorKindMask == colorKindDefault }

// Index returns the palette index for indexed colors.
func (c Color) Index() (uint8, bool) {
	if c&colorKindMask == colorKindIndexed {
		return uint8(c), true
	}
	return 0, false
}

// RGB returns the components of a direct color.
func (c Color) RGB() (RGB, bool) {
	if c&colorKindMask == colorKindRGB {
		return RGB{uint8(c >> 16), uint8(c >> 8), uint8(c)}, true
	}
	return RGB{}, false
}

func (c Color) String() string {
	if i, ok := c.Index(); ok {
		return strconv.Itoa(int(i))
	}
	if rgb, ok := c.RGB(); ok {
		return rgb.Hex()
	}
	return "default"
}

// RGB is a concrete 24-bit color.
type RGB struct{ R, G, B uint8 }

// Hex formats the color as #rrggbb.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// Luma is the relative brightness in 0..1 (Rec. 709 weights, no gamma).
func (c RGB) Luma() float64 {
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// Mix blends c toward o by t (0 = c, 1 = o).
func (c RGB) Mix(o RGB, t float64) RGB {
	m := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return RGB{m(c.R, o.R), m(c.G, o.G), m(c.B, o.B)}
}

// xtermSpec formats c the way xterm answers color queries: rgb:rrrr/gggg/bbbb.
func (c RGB) xtermSpec() string {
	return fmt.Sprintf("rgb:%02x%02x/%02x%02x/%02x%02x", c.R, c.R, c.G, c.G, c.B, c.B)
}

// ParseColorSpec understands the color syntaxes programs send in OSC 4/10/11:
// #rgb, #rrggbb, #rrrgggbbb, #rrrrggggbbbb, rgb:r/g/b (1 to 4 hex digits per
// component), and a handful of X11 names.
func ParseColorSpec(s string) (RGB, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 0 || len(h)%3 != 0 || len(h) > 12 {
			return RGB{}, false
		}
		n := len(h) / 3
		var out [3]uint8
		for i := 0; i < 3; i++ {
			v, ok := scaleHex(h[i*n : (i+1)*n])
			if !ok {
				return RGB{}, false
			}
			out[i] = v
		}
		return RGB{out[0], out[1], out[2]}, true
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "rgb:"); ok {
		parts := strings.Split(rest, "/")
		if len(parts) != 3 {
			return RGB{}, false
		}
		var out [3]uint8
		for i, p := range parts {
			v, ok := scaleHex(p)
			if !ok {
				return RGB{}, false
			}
			out[i] = v
		}
		return RGB{out[0], out[1], out[2]}, true
	}
	if c, ok := x11Names[strings.ToLower(strings.ReplaceAll(s, " ", ""))]; ok {
		return c, true
	}
	return RGB{}, false
}

// scaleHex converts 1-4 hex digits to 8 bits, as X11 does (f → ff, fff → ff).
func scaleHex(h string) (uint8, bool) {
	if len(h) == 0 || len(h) > 4 {
		return 0, false
	}
	v, err := strconv.ParseUint(h, 16, 16)
	if err != nil {
		return 0, false
	}
	max := uint64(1)<<(4*len(h)) - 1
	return uint8((v*255 + max/2) / max), true
}

var x11Names = map[string]RGB{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0}, "green": {0, 255, 0},
	"blue": {0, 0, 255}, "yellow": {255, 255, 0}, "cyan": {0, 255, 255}, "magenta": {255, 0, 255},
	"gray": {190, 190, 190}, "grey": {190, 190, 190}, "orange": {255, 165, 0}, "purple": {160, 32, 240},
}

// Palette is the full set of colors a terminal renders with.
type Palette struct {
	ANSI       [256]RGB
	Foreground RGB
	Background RGB
	Cursor     RGB
	CursorText RGB
	// Selection colors. When SelectionFG is unset the text keeps its color.
	SelectionBG    RGB
	SelectionFG    RGB
	HasSelectionFG bool
}

// DefaultPalette is xterm's palette with a neutral dark background, used
// when no theme supplies one.
func DefaultPalette() Palette {
	var p Palette
	base := [16]RGB{
		{0x1d, 0x1f, 0x21}, {0xcc, 0x66, 0x66}, {0xb5, 0xbd, 0x68}, {0xf0, 0xc6, 0x74},
		{0x81, 0xa2, 0xbe}, {0xb2, 0x94, 0xbb}, {0x8a, 0xbe, 0xb7}, {0xc5, 0xc8, 0xc6},
		{0x66, 0x66, 0x66}, {0xd5, 0x4e, 0x53}, {0xb9, 0xca, 0x4a}, {0xe7, 0xc5, 0x47},
		{0x7a, 0xa6, 0xda}, {0xc3, 0x97, 0xd8}, {0x70, 0xc0, 0xb1}, {0xea, 0xea, 0xea},
	}
	copy(p.ANSI[:16], base[:])
	FillXterm256(&p.ANSI)
	p.Foreground = RGB{0xe6, 0xe6, 0xe6}
	p.Background = RGB{0x16, 0x16, 0x1a}
	p.Cursor = RGB{0xe6, 0xe6, 0xe6}
	p.CursorText = p.Background
	p.SelectionBG = RGB{0x44, 0x47, 0x5a}
	return p
}

// FillXterm256 fills entries 16-255 with the standard 6x6x6 cube and the
// 24-step gray ramp.
func FillXterm256(a *[256]RGB) {
	steps := [6]uint8{0, 95, 135, 175, 215, 255}
	for i := 0; i < 216; i++ {
		a[16+i] = RGB{steps[i/36], steps[(i/6)%6], steps[i%6]}
	}
	for i := 0; i < 24; i++ {
		v := uint8(8 + 10*i)
		a[232+i] = RGB{v, v, v}
	}
}

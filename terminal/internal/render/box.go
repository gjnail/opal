package render

import (
	"image"
	"math"

	"golang.org/x/image/vector"
)

// Box drawing, block elements, braille, sextants and powerline separators
// are drawn from geometry instead of the font: fonts rarely make them
// line up across cells, and gaps between tiles are very visible.

type boxKey struct {
	r     rune
	cells uint8
}

func isBoxRune(r rune) bool {
	return (r >= 0x2500 && r <= 0x259f) || (r >= 0x2800 && r <= 0x28ff) ||
		(r >= 0xe0b0 && r <= 0xe0bf) || (r >= 0x1fb00 && r <= 0x1fb3b)
}

// boxGlyph returns a coverage mask for r, or nil to use the font.
func (r *Renderer) boxGlyph(ch rune, cells int) *image.Alpha {
	if !isBoxRune(ch) {
		return nil
	}
	k := boxKey{ch, uint8(cells)}
	if m, ok := r.boxes[k]; ok {
		return m
	}
	c := newCanvas(cells*r.m.CellW, r.m.CellH, r.m.UnderlineThick)
	if !c.draw(ch) {
		c.mask = nil
	}
	r.boxes[k] = c.mask
	return c.mask
}

type canvas struct {
	mask   *image.Alpha
	w, h   int
	lt, ht int // light and heavy stroke widths
}

func newCanvas(w, h, ul int) *canvas {
	lt := max(1, ul, int(math.Round(float64(h)/18)))
	ht := max(lt+1, lt*2)
	return &canvas{mask: image.NewAlpha(image.Rect(0, 0, w, h)), w: w, h: h, lt: lt, ht: ht}
}

// rect fills an integer rectangle.
func (c *canvas) rect(x0, y0, x1, y1 int, a uint8) {
	r := image.Rect(x0, y0, x1, y1).Intersect(c.mask.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := c.mask.PixOffset(x, y)
			if c.mask.Pix[i] < a {
				c.mask.Pix[i] = a
			}
		}
	}
}

// fracRect fills a rectangle given in fractions of the cell.
func (c *canvas) fracRect(fx0, fy0, fx1, fy1 float64) {
	x0 := int(math.Round(fx0 * float64(c.w)))
	x1 := int(math.Round(fx1 * float64(c.w)))
	y0 := int(math.Round(fy0 * float64(c.h)))
	y1 := int(math.Round(fy1 * float64(c.h)))
	c.rect(x0, y0, x1, y1, 255)
}

// poly fills a polygon (pixel coordinates) with antialiasing, adding to
// what's already drawn.
func (c *canvas) poly(pts ...[2]float32) {
	if len(pts) < 3 {
		return
	}
	r := vector.NewRasterizer(c.w, c.h)
	r.MoveTo(pts[0][0], pts[0][1])
	for _, p := range pts[1:] {
		r.LineTo(p[0], p[1])
	}
	r.ClosePath()
	c.addRaster(r)
}

func (c *canvas) addRaster(r *vector.Rasterizer) {
	tmp := image.NewAlpha(c.mask.Rect)
	r.Draw(tmp, tmp.Rect, image.Opaque, image.Point{})
	for i, a := range tmp.Pix {
		if a > c.mask.Pix[i] {
			c.mask.Pix[i] = a
		}
	}
}

// stroke draws a polyline of width t.
func (c *canvas) stroke(t float32, pts ...[2]float32) {
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l == 0 {
			continue
		}
		nx, ny := -dy/l*t/2, dx/l*t/2
		c.poly([2]float32{a[0] + nx, a[1] + ny}, [2]float32{b[0] + nx, b[1] + ny},
			[2]float32{b[0] - nx, b[1] - ny}, [2]float32{a[0] - nx, a[1] - ny})
	}
}

func (c *canvas) draw(ch rune) bool {
	switch {
	case ch >= 0x2500 && ch <= 0x257f:
		return c.boxLine(ch)
	case ch >= 0x2580 && ch <= 0x259f:
		return c.block(ch)
	case ch >= 0x2800 && ch <= 0x28ff:
		c.braille(int(ch - 0x2800))
		return true
	case ch >= 0x1fb00 && ch <= 0x1fb3b:
		v := int(ch-0x1fb00) + 1
		if v >= 21 {
			v++
		}
		if v >= 42 {
			v++
		}
		c.sextant(v)
		return true
	case ch >= 0xe0b0 && ch <= 0xe0bf:
		return c.powerline(ch)
	}
	return false
}

// Line weights for U+2500-U+257F, as up/right/down/left digits:
// 0 none, 1 light, 2 heavy, 3 double.
var boxWeights = map[rune]string{
	0x2500: "0101", 0x2501: "0202", 0x2502: "1010", 0x2503: "2020",
	0x250c: "0110", 0x250d: "0210", 0x250e: "0120", 0x250f: "0220",
	0x2510: "0011", 0x2511: "0012", 0x2512: "0021", 0x2513: "0022",
	0x2514: "1100", 0x2515: "1200", 0x2516: "2100", 0x2517: "2200",
	0x2518: "1001", 0x2519: "1002", 0x251a: "2001", 0x251b: "2002",
	0x251c: "1110", 0x251d: "1210", 0x251e: "2110", 0x251f: "1120",
	0x2520: "2120", 0x2521: "2210", 0x2522: "1220", 0x2523: "2220",
	0x2524: "1011", 0x2525: "1012", 0x2526: "2011", 0x2527: "1021",
	0x2528: "2021", 0x2529: "2012", 0x252a: "1022", 0x252b: "2022",
	0x252c: "0111", 0x252d: "0112", 0x252e: "0211", 0x252f: "0212",
	0x2530: "0121", 0x2531: "0122", 0x2532: "0221", 0x2533: "0222",
	0x2534: "1101", 0x2535: "1102", 0x2536: "1201", 0x2537: "1202",
	0x2538: "2101", 0x2539: "2102", 0x253a: "2201", 0x253b: "2202",
	0x253c: "1111", 0x253d: "1112", 0x253e: "1211", 0x253f: "1212",
	0x2540: "2111", 0x2541: "1121", 0x2542: "2121", 0x2543: "2112",
	0x2544: "2211", 0x2545: "1122", 0x2546: "1221", 0x2547: "2212",
	0x2548: "1222", 0x2549: "2122", 0x254a: "2221", 0x254b: "2222",
	0x2550: "0303", 0x2551: "3030", 0x2552: "0310", 0x2553: "0130",
	0x2554: "0330", 0x2555: "0013", 0x2556: "0031", 0x2557: "0033",
	0x2558: "1300", 0x2559: "3100", 0x255a: "3300", 0x255b: "1003",
	0x255c: "3001", 0x255d: "3003", 0x255e: "1310", 0x255f: "3130",
	0x2560: "3330", 0x2561: "1013", 0x2562: "3031", 0x2563: "3033",
	0x2564: "0313", 0x2565: "0131", 0x2566: "0333", 0x2567: "1303",
	0x2568: "3101", 0x2569: "3303", 0x256a: "1313", 0x256b: "3131",
	0x256c: "3333",
	0x2574: "0001", 0x2575: "1000", 0x2576: "0100", 0x2577: "0010",
	0x2578: "0002", 0x2579: "2000", 0x257a: "0200", 0x257b: "0020",
	0x257c: "0201", 0x257d: "1020", 0x257e: "0102", 0x257f: "2010",
}

func (c *canvas) boxLine(ch rune) bool {
	switch {
	case ch >= 0x2504 && ch <= 0x250b:
		i := int(ch - 0x2504)
		n := 3
		if i >= 4 {
			n = 4
		}
		c.dashes(n, i%2 == 1, i%4 >= 2)
		return true
	case ch >= 0x254c && ch <= 0x254f:
		i := int(ch - 0x254c)
		c.dashes(2, i%2 == 1, i >= 2)
		return true
	case ch >= 0x256d && ch <= 0x2570:
		c.arc(ch)
		return true
	case ch >= 0x2571 && ch <= 0x2573:
		t := float32(c.lt) * 1.15
		w, h := float32(c.w), float32(c.h)
		if ch != 0x2572 {
			c.stroke(t, [2]float32{w, 0}, [2]float32{0, h})
		}
		if ch != 0x2571 {
			c.stroke(t, [2]float32{0, 0}, [2]float32{w, h})
		}
		return true
	}
	s, ok := boxWeights[ch]
	if !ok {
		return false
	}
	c.lines(int(s[0]-'0'), int(s[1]-'0'), int(s[2]-'0'), int(s[3]-'0'))
	return true
}

// span is the width a stroke of weight wt takes across its axis.
func (c *canvas) span(wt int) int {
	switch wt {
	case 1:
		return c.lt
	case 2:
		return c.ht
	case 3:
		return 3 * c.lt
	}
	return 0
}

func (c *canvas) thick(wt int) int {
	if wt == 2 {
		return c.ht
	}
	return c.lt
}

// cx and cy position a stroke of width t centered in the cell.
func (c *canvas) cx(t int) int { return (c.w - t) / 2 }
func (c *canvas) cy(t int) int { return (c.h - t) / 2 }

// lines draws the four arms of a box-drawing character.
func (c *canvas) lines(up, right, down, left int) {
	lt, d := c.lt, c.lt
	tv := max(c.span(up), c.span(down)) // vertical stroke width at the center
	th := max(c.span(left), c.span(right))
	single := func(w int) bool { return w == 1 || w == 2 }
	// Where a horizontal double arm's top/bottom rail starts next to a
	// vertical stroke of weight v (in the direction the rail faces).
	railStart := func(near, far int) int {
		switch {
		case near == 3:
			return c.cx(lt) + d
		case single(near):
			return c.cx(c.thick(near))
		case far == 3:
			return c.cx(lt) - d
		case single(far):
			return c.cx(c.thick(far))
		}
		return c.cx(lt) - d
	}
	railEnd := func(near, far int) int {
		switch {
		case near == 3:
			return c.cx(lt) - d + lt
		case single(near):
			return c.cx(c.thick(near)) + c.thick(near)
		case far == 3:
			return c.cx(lt) + d + lt
		case single(far):
			return c.cx(c.thick(far)) + c.thick(far)
		}
		return c.cx(lt) + d + lt
	}
	colStart := func(near, far int) int {
		switch {
		case near == 3:
			return c.cy(lt) + d
		case single(near):
			return c.cy(c.thick(near))
		case far == 3:
			return c.cy(lt) - d
		case single(far):
			return c.cy(c.thick(far))
		}
		return c.cy(lt) - d
	}
	colEnd := func(near, far int) int {
		switch {
		case near == 3:
			return c.cy(lt) - d + lt
		case single(near):
			return c.cy(c.thick(near)) + c.thick(near)
		case far == 3:
			return c.cy(lt) + d + lt
		case single(far):
			return c.cy(c.thick(far)) + c.thick(far)
		}
		return c.cy(lt) + d + lt
	}

	if right == 3 {
		c.rect(railStart(up, down), c.cy(lt)-d, c.w, c.cy(lt)-d+lt, 255)
		c.rect(railStart(down, up), c.cy(lt)+d, c.w, c.cy(lt)+d+lt, 255)
	} else if right > 0 {
		t := c.thick(right)
		x0 := c.cx(max(tv, t))
		if tv == 0 {
			x0 = c.cx(t)
		}
		c.rect(x0, c.cy(t), c.w, c.cy(t)+t, 255)
	}
	if left == 3 {
		c.rect(0, c.cy(lt)-d, railEnd(up, down), c.cy(lt)-d+lt, 255)
		c.rect(0, c.cy(lt)+d, railEnd(down, up), c.cy(lt)+d+lt, 255)
	} else if left > 0 {
		t := c.thick(left)
		T := max(tv, t)
		c.rect(0, c.cy(t), c.cx(T)+T, c.cy(t)+t, 255)
	}
	if down == 3 {
		c.rect(c.cx(lt)-d, colStart(left, right), c.cx(lt)-d+lt, c.h, 255)
		c.rect(c.cx(lt)+d, colStart(right, left), c.cx(lt)+d+lt, c.h, 255)
	} else if down > 0 {
		t := c.thick(down)
		y0 := c.cy(max(th, t))
		if th == 0 {
			y0 = c.cy(t)
		}
		c.rect(c.cx(t), y0, c.cx(t)+t, c.h, 255)
	}
	if up == 3 {
		c.rect(c.cx(lt)-d, 0, c.cx(lt)-d+lt, colEnd(left, right), 255)
		c.rect(c.cx(lt)+d, 0, c.cx(lt)+d+lt, colEnd(right, left), 255)
	} else if up > 0 {
		t := c.thick(up)
		T := max(th, t)
		c.rect(c.cx(t), 0, c.cx(t)+t, c.cy(T)+T, 255)
	}
}

// dashes draws n dashes across (horizontal) or down (vertical) the cell.
func (c *canvas) dashes(n int, heavy, vertical bool) {
	t := c.lt
	if heavy {
		t = c.ht
	}
	length := c.w
	if vertical {
		length = c.h
	}
	seg := float64(length) / float64(n)
	gap := max(1, int(math.Round(seg*0.3)))
	for i := 0; i < n; i++ {
		a := int(math.Round(float64(i)*seg)) + gap/2
		b := int(math.Round(float64(i+1)*seg)) - (gap - gap/2)
		if vertical {
			c.rect(c.cx(t), a, c.cx(t)+t, b, 255)
		} else {
			c.rect(a, c.cy(t), b, c.cy(t)+t, 255)
		}
	}
}

// arc draws the rounded corners ╭╮╯╰ as a quarter circle joined to
// straight arms that line up with ─ and │.
func (c *canvas) arc(ch rune) {
	lt := c.lt
	cxI, cyI := c.cx(lt), c.cy(lt)
	cxf, cyf := float32(cxI)+float32(lt)/2, float32(cyI)+float32(lt)/2
	r := float32(math.Min(float64(c.w)/2, float64(c.h)/2))
	// Signs of the directions the corner opens toward.
	var sx, sy float32
	switch ch {
	case 0x256d: // down and right
		sx, sy = 1, 1
	case 0x256e: // down and left
		sx, sy = -1, 1
	case 0x256f: // up and left
		sx, sy = -1, -1
	case 0x2570: // up and right
		sx, sy = 1, -1
	}
	ox, oy := cxf+sx*r, cyf+sy*r // center of curvature
	const steps = 24
	var outer, inner [][2]float32
	ro, ri := r+float32(lt)/2, r-float32(lt)/2
	for i := 0; i <= steps; i++ {
		a := float64(i) / steps * math.Pi / 2
		dx, dy := -sx*float32(math.Cos(a)), -sy*float32(math.Sin(a))
		outer = append(outer, [2]float32{ox + dx*ro, oy + dy*ro})
		inner = append(inner, [2]float32{ox + dx*ri, oy + dy*ri})
	}
	pts := append([][2]float32{}, outer...)
	for i := len(inner) - 1; i >= 0; i-- {
		pts = append(pts, inner[i])
	}
	c.poly(pts...)
	// Straight arms from the arc to the cell edges.
	if sy > 0 {
		c.rect(cxI, int(cyf+r), cxI+lt, c.h, 255)
	} else {
		c.rect(cxI, 0, cxI+lt, int(math.Ceil(float64(cyf-r))), 255)
	}
	if sx > 0 {
		c.rect(int(cxf+r), cyI, c.w, cyI+lt, 255)
	} else {
		c.rect(0, cyI, int(math.Ceil(float64(cxf-r))), cyI+lt, 255)
	}
}

func (c *canvas) block(ch rune) bool {
	e := func(n int) float64 { return float64(n) / 8 }
	switch {
	case ch == 0x2580:
		c.fracRect(0, 0, 1, 0.5)
	case ch >= 0x2581 && ch <= 0x2588:
		n := int(ch-0x2581) + 1
		c.fracRect(0, 1-e(n), 1, 1)
	case ch >= 0x2589 && ch <= 0x258f:
		n := 7 - int(ch-0x2589)
		c.fracRect(0, 0, e(n), 1)
	case ch == 0x2590:
		c.fracRect(0.5, 0, 1, 1)
	case ch >= 0x2591 && ch <= 0x2593:
		a := uint8(64 * int(ch-0x2590))
		c.rect(0, 0, c.w, c.h, a)
	case ch == 0x2594:
		c.fracRect(0, 0, 1, e(1))
	case ch == 0x2595:
		c.fracRect(1-e(1), 0, 1, 1)
	default:
		// Quadrants: bits are upper-left, upper-right, lower-left, lower-right.
		q := map[rune]int{0x2596: 4, 0x2597: 8, 0x2598: 1, 0x2599: 1 | 4 | 8, 0x259a: 1 | 8,
			0x259b: 1 | 2 | 4, 0x259c: 1 | 2 | 8, 0x259d: 2, 0x259e: 2 | 4, 0x259f: 2 | 4 | 8}[ch]
		if q&1 != 0 {
			c.fracRect(0, 0, 0.5, 0.5)
		}
		if q&2 != 0 {
			c.fracRect(0.5, 0, 1, 0.5)
		}
		if q&4 != 0 {
			c.fracRect(0, 0.5, 0.5, 1)
		}
		if q&8 != 0 {
			c.fracRect(0.5, 0.5, 1, 1)
		}
	}
	return true
}

func (c *canvas) sextant(v int) {
	for i := 0; i < 6; i++ {
		if v&(1<<i) != 0 {
			col, row := float64(i%2), float64(i/2)
			c.fracRect(col/2, row/3, (col+1)/2, (row+1)/3)
		}
	}
}

// braille draws the 2x4 dot grid; bit order follows Unicode (dots 1-3
// down the left column, 4-6 down the right, then 7 and 8 below).
func (c *canvas) braille(bits int) {
	pos := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	cw, chh := float64(c.w)/2, float64(c.h)/4
	rad := math.Max(0.8, math.Min(cw, chh)*0.28)
	for i, p := range pos {
		if bits&(1<<i) == 0 {
			continue
		}
		cx := (float64(p[0]) + 0.5) * cw
		cy := (float64(p[1]) + 0.5) * chh
		var pts [][2]float32
		for k := 0; k < 16; k++ {
			a := float64(k) / 16 * 2 * math.Pi
			pts = append(pts, [2]float32{float32(cx + rad*math.Cos(a)), float32(cy + rad*math.Sin(a))})
		}
		c.poly(pts...)
	}
}

// powerline draws the separators U+E0B0-E0BF. They bleed a fraction of a
// pixel past the cell so no seam shows against the neighbouring block.
func (c *canvas) powerline(ch rune) bool {
	w, h := float32(c.w), float32(c.h)
	t := float32(c.lt)
	half := func(right, solid bool) {
		const steps = 32
		var pts [][2]float32
		cx := float32(0)
		sign := float32(1)
		if !right {
			cx, sign = w, -1
		}
		for i := 0; i <= steps; i++ {
			a := -math.Pi/2 + float64(i)/steps*math.Pi
			pts = append(pts, [2]float32{cx + sign*w*float32(math.Cos(a)), h/2 + h/2*float32(math.Sin(a))})
		}
		if solid {
			c.poly(pts...)
		} else {
			c.stroke(t, pts...)
		}
	}
	switch ch {
	case 0xe0b0:
		c.poly([2]float32{0, -0.5}, [2]float32{w, h / 2}, [2]float32{0, h + 0.5})
	case 0xe0b1:
		c.stroke(t, [2]float32{0, 0}, [2]float32{w - t/2, h / 2}, [2]float32{0, h})
	case 0xe0b2:
		c.poly([2]float32{w, -0.5}, [2]float32{0, h / 2}, [2]float32{w, h + 0.5})
	case 0xe0b3:
		c.stroke(t, [2]float32{w, 0}, [2]float32{t / 2, h / 2}, [2]float32{w, h})
	case 0xe0b4:
		half(true, true)
	case 0xe0b5:
		half(true, false)
	case 0xe0b6:
		half(false, true)
	case 0xe0b7:
		half(false, false)
	case 0xe0b8:
		c.poly([2]float32{0, 0}, [2]float32{w, h}, [2]float32{0, h})
	case 0xe0b9, 0xe0bf:
		c.stroke(t, [2]float32{0, 0}, [2]float32{w, h})
	case 0xe0ba:
		c.poly([2]float32{w, 0}, [2]float32{w, h}, [2]float32{0, h})
	case 0xe0bb, 0xe0bd:
		c.stroke(t, [2]float32{0, h}, [2]float32{w, 0})
	case 0xe0bc:
		c.poly([2]float32{0, 0}, [2]float32{w, 0}, [2]float32{0, h})
	case 0xe0be:
		c.poly([2]float32{0, 0}, [2]float32{w, 0}, [2]float32{w, h})
	default:
		return false
	}
	return true
}

package vt

import (
	"image"
	"math"
)

// Sixel graphics (DCS P1;P2;P3 q ... ST), as defined for the VT340 and
// extended by xterm.

const sixelMaxSide = 4096

// vt340Palette is the VT340's default sixel palette, in percent RGB.
var vt340Palette = [16][3]int{
	{0, 0, 0}, {20, 20, 80}, {80, 13, 13}, {20, 80, 20}, {80, 20, 80}, {20, 80, 80}, {80, 80, 20}, {53, 53, 53},
	{26, 26, 26}, {33, 33, 60}, {60, 26, 26}, {33, 60, 33}, {60, 33, 60}, {33, 60, 60}, {60, 60, 33}, {80, 80, 80},
}

type sixelDecoder struct {
	t           *Terminal
	pal         [256][4]uint8
	color       int
	x, y        int // y is the top of the current six-pixel band
	w, h        int // extent drawn so far
	stride      int
	pix         []uint8 // RGBA, stride*4 bytes per row
	transparent bool

	// Command parsing.
	cmd     byte // '#', '!', '"' or 0
	params  [5]int
	nparams int
	repeat  int
	tooBig  bool
}

func pct(v int) uint8 { return uint8(clamp(v, 0, 100) * 255 / 100) }

func (t *Terminal) newSixel(p *params) dcsHandler {
	d := &sixelDecoder{t: t, repeat: 1}
	// P2 = 1 means pixels not drawn stay transparent; otherwise they get
	// color 0.
	d.transparent = p.get(1, 0) == 1
	for i, c := range vt340Palette {
		d.pal[i] = [4]uint8{pct(c[0]), pct(c[1]), pct(c[2]), 255}
	}
	for i := 16; i < 256; i++ {
		d.pal[i] = [4]uint8{0, 0, 0, 255}
	}
	return d
}

// grow makes the buffer at least w x h pixels.
func (d *sixelDecoder) grow(w, h int) bool {
	if w > sixelMaxSide || h > sixelMaxSide {
		d.tooBig = true
		return false
	}
	oldRows := 0
	if d.stride > 0 {
		oldRows = len(d.pix) / (4 * d.stride)
	}
	if w <= d.stride && h <= oldRows {
		return true
	}
	// Grow geometrically so tall images don't copy every band.
	stride := max(d.stride, 64)
	for stride < w {
		stride *= 2
	}
	stride = min(stride, sixelMaxSide)
	rows := oldRows
	if h > rows {
		rows = min(max(h, 2*oldRows, 6), sixelMaxSide)
	}
	pix := make([]uint8, stride*rows*4)
	for y := 0; y < oldRows; y++ {
		copy(pix[y*stride*4:], d.pix[y*d.stride*4:(y+1)*d.stride*4])
	}
	d.pix, d.stride = pix, stride
	return true
}

func (d *sixelDecoder) put(b []byte) {
	for _, c := range b {
		if d.tooBig {
			return
		}
		if d.cmd != 0 {
			if c >= '0' && c <= '9' {
				if d.nparams == 0 {
					d.nparams = 1
				}
				v := &d.params[d.nparams-1]
				if *v < 100000 {
					*v = *v*10 + int(c-'0')
				}
				continue
			}
			if c == ';' {
				if d.nparams == 0 {
					d.nparams = 1
				}
				if d.nparams < len(d.params) {
					d.nparams++
					d.params[d.nparams-1] = 0
				}
				continue
			}
			d.finishCommand()
		}
		switch {
		case c == '#' || c == '!' || c == '"':
			d.cmd = c
			d.nparams = 0
			d.params = [5]int{}
		case c == '$':
			d.x = 0
		case c == '-':
			d.x = 0
			d.y += 6
		case c >= 0x3f && c <= 0x7e:
			d.sixel(c - 0x3f)
		}
	}
}

func (d *sixelDecoder) finishCommand() {
	p := d.params
	switch d.cmd {
	case '!':
		d.repeat = max(1, p[0])
	case '#':
		idx := clamp(p[0], 0, 255)
		if d.nparams >= 5 {
			switch p[1] {
			case 1: // HLS; DEC puts blue at 0 degrees
				r, g, b := hlsToRGB(float64((p[2]+240)%360), float64(p[3])/100, float64(p[4])/100)
				d.pal[idx] = [4]uint8{r, g, b, 255}
			case 2:
				d.pal[idx] = [4]uint8{pct(p[2]), pct(p[3]), pct(p[4]), 255}
			}
		}
		d.color = idx
	case '"':
		// Raster attributes: aspect (ignored) and the image size, which
		// lets us allocate once.
		if d.nparams >= 4 && p[2] > 0 && p[3] > 0 {
			if d.grow(p[2], p[3]) {
				d.w, d.h = max(d.w, p[2]), max(d.h, p[3])
			}
		}
	}
	d.cmd = 0
}

func (d *sixelDecoder) sixel(bits byte) {
	n := d.repeat
	d.repeat = 1
	if bits == 0 {
		d.x += n
		return
	}
	if !d.grow(d.x+n, d.y+6) {
		return
	}
	col := d.pal[d.color]
	for bit := 0; bit < 6; bit++ {
		if bits&(1<<bit) == 0 {
			continue
		}
		y := d.y + bit
		row := d.pix[y*d.stride*4:]
		for x := d.x; x < d.x+n; x++ {
			copy(row[x*4:x*4+4], col[:])
		}
		d.h = max(d.h, y+1)
	}
	d.x += n
	d.w = max(d.w, d.x)
}

func (d *sixelDecoder) unhook(t *Terminal) {
	if d.cmd != 0 {
		d.finishCommand()
	}
	if d.tooBig || d.w == 0 || d.h == 0 {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, d.w, d.h))
	bg := d.pal[0]
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			var px []uint8
			if y*d.stride*4+x*4+4 <= len(d.pix) && x < d.stride {
				px = d.pix[y*d.stride*4+x*4 : y*d.stride*4+x*4+4]
			}
			o := img.PixOffset(x, y)
			switch {
			case px != nil && px[3] != 0:
				copy(img.Pix[o:o+4], px)
			case !d.transparent:
				copy(img.Pix[o:o+4], bg[:])
			}
		}
	}
	cols, rows := t.cellsFor(d.w, d.h)
	pl := &Placement{Image: newImage(img), Cols: cols, Rows: rows, Src: img.Rect,
		W: float32(d.w) / float32(t.cellW), H: float32(d.h) / float32(t.cellH)}
	if t.modes.get(modeSixelDisplay) {
		// Sixel scrolling off: the image goes at the top-left and the
		// cursor stays put.
		sx, sy := t.cur.x, t.cur.y
		t.cur.x, t.cur.y = 0, 0
		t.place(pl, true)
		t.cur.x, t.cur.y = sx, sy
		return
	}
	t.place(pl, false)
	if !t.modes.get(modeSixelCursorRight) {
		t.carriageReturn()
		t.index()
	}
}

// hlsToRGB converts hue (degrees), lightness and saturation (0-1).
func hlsToRGB(h, l, s float64) (uint8, uint8, uint8) {
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360
	conv := func(t float64) uint8 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 0.5:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return uint8(math.Round(v * 255))
	}
	return conv(hk + 1.0/3), conv(hk), conv(hk - 1.0/3)
}

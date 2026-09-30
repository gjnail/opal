package vt

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// The kitty graphics protocol: APC G <keys> ; <base64 payload> ST.
// https://sw.kovidgoyal.net/kitty/graphics-protocol/

const maxImageSide = 10000

type kittyCmd struct {
	action     byte // t, T, p, d, q
	format     int  // 24, 32, 100
	medium     byte // d, f, t, s
	width      int  // s: pixels, for raw formats
	height     int  // v
	size       int  // S: bytes to read from a file
	offset     int  // O
	id         uint32
	number     uint32
	placement  uint32
	compressed byte // o
	more       bool // m
	quiet      int  // q
	srcX, srcY int
	srcW, srcH int
	offX, offY int // X, Y: pixel offset within the first cell
	cols, rows int // c, r
	cursor     int // C: 1 = don't move the cursor
	z          int32
	delete     byte
	virtual    bool // U: unicode placeholders (not supported)
}

type kittyTransfer struct {
	cmd  kittyCmd
	data []byte
}

func parseKittyKeys(s string) (kittyCmd, error) {
	c := kittyCmd{action: 't', format: 32, medium: 'd'}
	for _, kv := range strings.Split(s, ",") {
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok || len(k) != 1 {
			return c, fmt.Errorf("bad key %q", kv)
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		ch := byte(0)
		if len(v) > 0 {
			ch = v[0]
		}
		switch k[0] {
		case 'a':
			c.action = ch
		case 'f':
			c.format = int(n)
		case 't':
			c.medium = ch
		case 's':
			c.width = int(n)
		case 'v':
			c.height = int(n)
		case 'S':
			c.size = int(n)
		case 'O':
			c.offset = int(n)
		case 'i':
			c.id = uint32(n)
		case 'I':
			c.number = uint32(n)
		case 'p':
			c.placement = uint32(n)
		case 'o':
			c.compressed = ch
		case 'm':
			c.more = n == 1
		case 'q':
			c.quiet = int(n)
		case 'x':
			c.srcX = int(n)
		case 'y':
			c.srcY = int(n)
		case 'w':
			c.srcW = int(n)
		case 'h':
			c.srcH = int(n)
		case 'X':
			c.offX = int(n)
		case 'Y':
			c.offY = int(n)
		case 'c':
			c.cols = int(n)
		case 'r':
			c.rows = int(n)
		case 'C':
			c.cursor = int(n)
		case 'z':
			c.z = int32(n)
		case 'd':
			c.delete = ch
		case 'U':
			c.virtual = n == 1
		}
	}
	return c, nil
}

// apcDispatch handles APC strings; kitty graphics are the only kind.
func (t *Terminal) apcDispatch(data []byte) {
	t.lastValid = false
	if len(data) == 0 || data[0] != 'G' {
		return
	}
	keys, payload, _ := bytes.Cut(data[1:], []byte{';'})
	cmd, err := parseKittyKeys(string(keys))
	st := t.images
	if pending := st.kitty; pending != nil {
		// A continuation chunk carries only m (and maybe q).
		dec, derr := base64.StdEncoding.DecodeString(string(payload))
		if derr != nil {
			dec, _ = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(payload), "="))
		}
		pending.data = append(pending.data, dec...)
		if len(pending.data) > maxAPC*2 {
			st.kitty = nil
			t.kittyReply(pending.cmd, "EFBIG:image data too large")
			return
		}
		if cmd.more {
			return
		}
		st.kitty = nil
		t.kittyExecute(pending.cmd, pending.data)
		return
	}
	if err != nil {
		return
	}
	var dec []byte
	if len(payload) > 0 {
		dec, err = base64.StdEncoding.DecodeString(string(payload))
		if err != nil {
			dec, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(payload), "="))
			if err != nil {
				t.kittyReply(cmd, "EINVAL:bad base64")
				return
			}
		}
	}
	if cmd.more {
		st.kitty = &kittyTransfer{cmd: cmd, data: dec}
		return
	}
	t.kittyExecute(cmd, dec)
}

// kittyReply answers a command unless the program asked for quiet. msg
// "" means OK.
func (t *Terminal) kittyReply(c kittyCmd, msg string) {
	if c.id == 0 && c.number == 0 {
		return
	}
	if msg == "" && c.quiet >= 1 || msg != "" && c.quiet >= 2 {
		return
	}
	var b strings.Builder
	b.WriteString("\x1b_G")
	if c.id != 0 {
		fmt.Fprintf(&b, "i=%d", c.id)
	}
	if c.number != 0 {
		if c.id != 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "I=%d", c.number)
	}
	if c.placement != 0 {
		fmt.Fprintf(&b, ",p=%d", c.placement)
	}
	b.WriteByte(';')
	if msg == "" {
		b.WriteString("OK")
	} else {
		b.WriteString(msg)
	}
	b.WriteString("\x1b\\")
	t.reply(b.String())
}

func (t *Terminal) kittyExecute(c kittyCmd, data []byte) {
	st := t.images
	switch c.action {
	case 't', 'T', 'q':
		if c.virtual {
			t.kittyReply(c, "ENOTSUP:unicode placeholders are not supported")
			return
		}
		pix, err := kittyDecode(c, data)
		if err != nil {
			t.kittyReply(c, err.Error())
			return
		}
		if c.action == 'q' {
			t.kittyReply(c, "")
			return
		}
		img := newImage(pix)
		if c.id == 0 {
			if c.number == 0 {
				// Anonymous and displayed right away: nothing can refer to
				// it later, so don't keep it in the store.
				if c.action == 'T' {
					t.kittyPlace(c, img)
				}
				return
			}
			st.nextImage++
			c.id = st.nextImage
		}
		img.ID, img.Number = c.id, c.number
		st.add(img)
		if c.action == 'T' {
			t.kittyPlace(c, img)
		}
		t.kittyReply(c, "")
	case 'p':
		img := st.byID[c.id]
		if c.id == 0 && c.number != 0 {
			img = st.byNumber[c.number]
		}
		if img == nil {
			t.kittyReply(c, "ENOENT:no such image")
			return
		}
		t.kittyPlace(c, img)
		t.kittyReply(c, "")
	case 'd':
		t.kittyDelete(c)
	}
}

// kittyDecode turns transmitted data into an image.
func kittyDecode(c kittyCmd, data []byte) (*image.RGBA, error) {
	switch c.medium {
	case 'd':
	case 'f', 't':
		path := string(data)
		if c.medium == 't' {
			// Only temporary files that clearly belong to this protocol
			// may be read and deleted, as kitty requires.
			if !strings.Contains(path, "tty-graphics-protocol") || !strings.HasPrefix(filepath.Clean(path), filepath.Clean(os.TempDir())) {
				return nil, fmt.Errorf("EPERM:not a temporary file")
			}
			defer os.Remove(path)
		}
		b, err := readRegular(path, c.offset, c.size)
		if err != nil {
			return nil, fmt.Errorf("EBADF:%v", err)
		}
		data = b
	default:
		return nil, fmt.Errorf("ENOTSUP:transmission medium not supported")
	}
	if c.compressed == 'z' {
		r, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("EINVAL:bad zlib data")
		}
		out, err := io.ReadAll(io.LimitReader(r, 4*maxImageSide*maxImageSide))
		if err != nil {
			return nil, fmt.Errorf("EINVAL:bad zlib data")
		}
		data = out
	}
	switch c.format {
	case 100:
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("EBADPNG:%v", err)
		}
		if cfg.Width > maxImageSide || cfg.Height > maxImageSide {
			return nil, fmt.Errorf("EFBIG:image too large")
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("EBADPNG:%v", err)
		}
		return toRGBA(img), nil
	case 24, 32:
		bpp := c.format / 8
		w, h := c.width, c.height
		if w <= 0 || h <= 0 || w > maxImageSide || h > maxImageSide {
			return nil, fmt.Errorf("EINVAL:bad image size")
		}
		if len(data) < w*h*bpp {
			return nil, fmt.Errorf("ENODATA:need %d bytes, have %d", w*h*bpp, len(data))
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < w*h; i++ {
			r, g, b := data[i*bpp], data[i*bpp+1], data[i*bpp+2]
			a := uint8(255)
			if bpp == 4 {
				a = data[i*bpp+3]
			}
			// image.RGBA is premultiplied.
			img.Pix[i*4] = uint8(uint32(r) * uint32(a) / 255)
			img.Pix[i*4+1] = uint8(uint32(g) * uint32(a) / 255)
			img.Pix[i*4+2] = uint8(uint32(b) * uint32(a) / 255)
			img.Pix[i*4+3] = a
		}
		return img, nil
	}
	return nil, fmt.Errorf("EINVAL:unknown format %d", c.format)
}

func readRegular(path string, offset, size int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if st.Size() > 256<<20 {
		return nil, fmt.Errorf("file too large")
	}
	if offset > 0 {
		if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
			return nil, err
		}
	}
	var r io.Reader = f
	if size > 0 {
		r = io.LimitReader(f, int64(size))
	}
	return io.ReadAll(r)
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

// kittyPlace shows an image at the cursor.
func (t *Terminal) kittyPlace(c kittyCmd, img *Image) {
	b := img.Pix.Rect
	src := image.Rect(c.srcX, c.srcY, c.srcX+c.srcW, c.srcY+c.srcH)
	if c.srcW == 0 {
		src.Max.X = b.Max.X
	}
	if c.srcH == 0 {
		src.Max.Y = b.Max.Y
	}
	src = src.Intersect(b)
	if src.Empty() {
		return
	}
	cols, rows := c.cols, c.rows
	natCols, natRows := t.cellsFor(src.Dx()+c.offX, src.Dy()+c.offY)
	switch {
	case cols == 0 && rows == 0:
		cols, rows = natCols, natRows
	case cols == 0:
		// Keep the aspect ratio from the given height.
		cols = max(1, (src.Dx()*rows*t.cellH+src.Dy()*t.cellW/2)/(src.Dy()*t.cellW))
	case rows == 0:
		rows = max(1, (src.Dy()*cols*t.cellW+src.Dx()*t.cellH/2)/(src.Dx()*t.cellH))
	}
	// A new placement with the same ids replaces the old one.
	if c.placement != 0 {
		t.deletePlacements(func(_ *Line, _ int, pl *Placement) bool {
			return pl.Image.ID == img.ID && pl.ID == c.placement && img.ID != 0
		})
	}
	pl := &Placement{Image: img, ID: c.placement, Cols: cols, Rows: rows, Src: src, OffX: c.offX, OffY: c.offY, Z: c.z}
	if c.cols == 0 && c.rows == 0 {
		pl.W, pl.H = float32(src.Dx())/float32(t.cellW), float32(src.Dy())/float32(t.cellH)
	}
	t.place(pl, c.cursor == 1)
}

// kittyDelete implements a=d. Lowercase keys remove placements; uppercase
// also frees the image data.
func (t *Terminal) kittyDelete(c kittyCmd) {
	st := t.images
	d := c.delete
	if d == 0 {
		d = 'a'
	}
	free := d >= 'A' && d <= 'Z'
	top := t.buf.screenBase(t.rows)
	onScreen := func(row int) bool { return row >= top }
	hits := func(row int, pl *Placement, x, y int) bool {
		sy := row - top
		return x >= pl.Col && x < pl.Col+pl.Cols && y >= sy && y < sy+pl.Rows
	}
	var freed []uint32
	switch d | 0x20 { // lowercase
	case 'a':
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			if onScreen(row) {
				freed = append(freed, pl.Image.ID)
				return true
			}
			return false
		})
	case 'i':
		t.deletePlacements(func(_ *Line, _ int, pl *Placement) bool {
			return pl.Image.ID == c.id && (c.placement == 0 || pl.ID == c.placement)
		})
		freed = append(freed, c.id)
	case 'n':
		if img := st.byNumber[c.number]; img != nil {
			t.deletePlacements(func(_ *Line, _ int, pl *Placement) bool { return pl.Image == img })
			freed = append(freed, img.ID)
		}
	case 'c':
		x, y := t.cur.x, t.cur.y
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			if hits(row, pl, x, y) {
				freed = append(freed, pl.Image.ID)
				return true
			}
			return false
		})
	case 'p':
		x, y := c.srcX-1, c.srcY-1
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			if hits(row, pl, x, y) {
				freed = append(freed, pl.Image.ID)
				return true
			}
			return false
		})
	case 'x':
		x := c.srcX - 1
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			return onScreen(row) && x >= pl.Col && x < pl.Col+pl.Cols
		})
	case 'y':
		y := c.srcY - 1
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			sy := row - top
			return onScreen(row) && y >= sy && y < sy+pl.Rows
		})
	case 'z':
		t.deletePlacements(func(_ *Line, row int, pl *Placement) bool {
			return onScreen(row) && pl.Z == c.z
		})
	}
	if free {
		for _, id := range freed {
			if id != 0 {
				st.drop(id)
			}
		}
	}
}

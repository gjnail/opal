package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func images(t *Terminal) []*Placement {
	var out []*Placement
	t.VisibleImages(0, t.buf.count(), func(_ int, pl *Placement) { out = append(out, pl) })
	return out
}

func TestSixel(t *testing.T) {
	term := newTerm(20, 10)
	term.SetCellSize(10, 20)
	// Color 1 is red; draw a 4x12 red block (two bands of 6 pixels).
	term.WriteString("ab\x1bPq\"1;1;4;12#1;2;100;0;0#1!4~-!4~\x1b\\x")
	pls := images(term)
	if len(pls) != 1 {
		t.Fatalf("placements = %d", len(pls))
	}
	pl := pls[0]
	if pl.Image.Pix.Rect.Dx() != 4 || pl.Image.Pix.Rect.Dy() != 12 || pl.Cols != 1 || pl.Rows != 1 || pl.Col != 2 {
		t.Fatalf("placement %+v size %v", pl, pl.Image.Pix.Rect)
	}
	if got := pl.Image.Pix.RGBAAt(1, 7); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("pixel = %v", got)
	}
	// The cursor moved to the next line; text continues there.
	if term.ScreenLine(1).String() != "x" {
		t.Fatalf("screen: %q", screen(term))
	}
}

func TestSixelTransparentAndLimits(t *testing.T) {
	term := newTerm(20, 10)
	term.WriteString("\x1bP0;1q#0;2;0;100;0!2@\x1b\\")
	pl := images(term)[0]
	if pl.Image.Pix.RGBAAt(0, 1).A != 0 || pl.Image.Pix.RGBAAt(0, 0).A != 255 {
		t.Fatal("P2=1 should leave undrawn pixels transparent")
	}
	term = newTerm(20, 10)
	term.WriteString("\x1bPq!99999~\x1b\\ok")
	if len(images(term)) != 0 {
		t.Fatal("an oversized sixel must be dropped")
	}
	if !strings.Contains(term.ScreenLine(0).String(), "ok") {
		t.Fatal("text after a dropped sixel must still print")
	}
}

func kittyRGB(w, h int) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{10, 20, 30}, w*h))
}

func TestKittyTransmitAndPlace(t *testing.T) {
	term := newTerm(40, 10)
	term.SetCellSize(10, 20)
	term.WriteString("\x1b_Ga=T,f=24,s=30,v=40,i=7;" + kittyRGB(30, 40) + "\x1b\\")
	if got := string(term.TakeReplies()); got != "\x1b_Gi=7;OK\x1b\\" {
		t.Fatalf("reply = %q", got)
	}
	pls := images(term)
	if len(pls) != 1 || pls[0].Cols != 3 || pls[0].Rows != 2 {
		t.Fatalf("placement = %+v", pls)
	}
	// Cursor: right of the image on its last row.
	if x, y := term.CursorPos(); x != 3 || y != 1 {
		t.Fatalf("cursor = %d,%d", x, y)
	}
	// Place it again, scaled to 6 columns, without moving the cursor.
	term.WriteString("\x1b_Ga=p,i=7,c=6,C=1,q=1\x1b\\")
	pls = images(term)
	if len(pls) != 2 || pls[1].Cols != 6 || pls[1].Rows != 4 {
		t.Fatalf("scaled placement = %+v", pls[1])
	}
	if x, y := term.CursorPos(); x != 3 || y != 1 {
		t.Fatal("C=1 must not move the cursor")
	}
	// Delete by id, freeing the data.
	term.WriteString("\x1b_Ga=d,d=I,i=7\x1b\\")
	if len(images(term)) != 0 || term.images.byID[7] != nil {
		t.Fatal("d=I should remove placements and data")
	}
}

func TestKittyChunkedPNGAndQuery(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	png.Encode(&buf, img)
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	term := newTerm(40, 10)
	term.SetCellSize(8, 16)
	mid := len(b64) / 2 / 4 * 4
	term.WriteString("\x1b_Ga=T,f=100,i=3,m=1;" + b64[:mid] + "\x1b\\")
	term.WriteString("\x1b_Gm=0;" + b64[mid:] + "\x1b\\")
	if got := string(term.TakeReplies()); got != "\x1b_Gi=3;OK\x1b\\" {
		t.Fatalf("reply = %q", got)
	}
	if pls := images(term); len(pls) != 1 || pls[0].Cols != 2 || pls[0].Rows != 1 {
		t.Fatalf("placement = %+v", pls)
	}
	// Programs detect support with a query, which stores nothing.
	term.WriteString("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\")
	if got := string(term.TakeReplies()); got != "\x1b_Gi=31;OK\x1b\\" {
		t.Fatalf("query reply = %q", got)
	}
	if term.images.byID[31] != nil {
		t.Fatal("a query must not store the image")
	}
	term.WriteString("\x1b_Ga=p,i=99\x1b\\")
	if got := string(term.TakeReplies()); !strings.HasPrefix(got, "\x1b_Gi=99;ENOENT") {
		t.Fatalf("missing image reply = %q", got)
	}
}

func TestITermInline(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 20, 40)))
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	term := newTerm(40, 10)
	term.SetCellSize(10, 20)
	term.WriteString("\x1b]1337;File=inline=1;width=4:" + data + "\x07after")
	pls := images(term)
	if len(pls) != 1 || pls[0].Cols != 4 || pls[0].Rows != 4 {
		t.Fatalf("placement = %+v", pls)
	}
	if term.ScreenLine(4).String() != "after" {
		t.Fatalf("text after image: %q", screen(term))
	}
	// Not inline: a download, which isn't shown.
	term.WriteString("\x1b]1337;File=name=eC5wbmc=:" + data + "\x07")
	if len(images(term)) != 1 {
		t.Fatal("non-inline files must not display")
	}
}

func TestImagesScrollAndReflow(t *testing.T) {
	term := newTerm(20, 4)
	term.SetCellSize(10, 20)
	term.WriteString("\x1b_Ga=T,f=24,s=10,v=20;" + kittyRGB(10, 20) + "\x1b\\")
	for i := 0; i < 6; i++ {
		term.WriteString("\r\nline")
	}
	// The image scrolled into history with its line.
	if term.HistoryLen() == 0 || len(term.LineAt(0).Images) != 1 {
		t.Fatal("image should be on the first history line")
	}
	term.Resize(10, 4)
	found := 0
	for i := 0; i < term.buf.count(); i++ {
		found += len(term.buf.line(i).Images)
	}
	if found != 1 {
		t.Fatalf("image lost in reflow (found %d)", found)
	}
	term.WriteString("\x1b[3J")
	for i := 0; i < term.buf.count(); i++ {
		if len(term.buf.line(i).Images) != 0 && i < term.HistoryLen() {
			t.Fatal("history images should go with ED 3")
		}
	}
}

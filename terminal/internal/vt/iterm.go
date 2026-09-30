package vt

import (
	"bytes"
	"encoding/base64"
	"image"
	"strconv"
	"strings"
)

// iTerm2 inline images: OSC 1337 ; File=key=value;... : <base64> ST, and
// the multipart form (MultipartFile, FilePart, FileEnd) newer versions use
// for large files.

type itermTransfer struct {
	args string
	data []byte
}

func (t *Terminal) itermImage(val string) {
	args, data, ok := strings.Cut(val, ":")
	if !ok {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		dec, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(data, "="))
		if err != nil {
			return
		}
	}
	t.itermShow(args, dec)
}

func (t *Terminal) itermMultipartStart(args string) {
	t.images.iterm = &itermTransfer{args: args}
}

func (t *Terminal) itermMultipartPart(b64 string) {
	tr := t.images.iterm
	if tr == nil {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.images.iterm = nil
		return
	}
	tr.data = append(tr.data, dec...)
	if len(tr.data) > maxOSC {
		t.images.iterm = nil
	}
}

func (t *Terminal) itermMultipartEnd() {
	tr := t.images.iterm
	t.images.iterm = nil
	if tr != nil {
		t.itermShow(tr.args, tr.data)
	}
}

func (t *Terminal) itermShow(args string, data []byte) {
	opts := map[string]string{}
	for _, kv := range strings.Split(args, ";") {
		k, v, _ := strings.Cut(kv, "=")
		opts[k] = v
	}
	// Without inline=1 it's a file download, which we don't offer.
	if opts["inline"] != "1" {
		return
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageSide || cfg.Height > maxImageSide {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	pix := toRGBA(img)
	w, h := pix.Rect.Dx(), pix.Rect.Dy()

	// Sizes are "auto", N cells, Npx or N% of the terminal.
	size := func(spec string, cell, total int) (px int, set bool) {
		switch {
		case spec == "" || spec == "auto":
			return 0, false
		case strings.HasSuffix(spec, "px"):
			n, _ := strconv.Atoi(strings.TrimSuffix(spec, "px"))
			return n, n > 0
		case strings.HasSuffix(spec, "%"):
			n, _ := strconv.Atoi(strings.TrimSuffix(spec, "%"))
			return total * n / 100, n > 0
		}
		n, _ := strconv.Atoi(spec)
		return n * cell, n > 0
	}
	tw, wset := size(opts["width"], t.cellW, t.cols*t.cellW)
	th, hset := size(opts["height"], t.cellH, t.rows*t.cellH)
	keepAspect := opts["preserveAspectRatio"] != "0"
	switch {
	case !wset && !hset:
		tw, th = w, h
		// Don't let an image be wider than the terminal.
		if maxW := t.cols * t.cellW; tw > maxW {
			th, tw = th*maxW/tw, maxW
		}
	case wset && !hset:
		th = h * tw / w
	case hset && !wset:
		tw = w * th / h
	case keepAspect:
		// Fit inside the box.
		if tw*h > th*w {
			tw = w * th / h
		} else {
			th = h * tw / w
		}
	}
	cols, rows := t.cellsFor(max(tw, 1), max(th, 1))
	pl := &Placement{Image: newImage(pix), Cols: cols, Rows: rows, Src: pix.Rect,
		W: float32(tw) / float32(t.cellW), H: float32(th) / float32(t.cellH)}
	keep := opts["doNotMoveCursor"] == "1"
	t.place(pl, keep)
	if !keep {
		t.carriageReturn()
		t.index()
	}
}

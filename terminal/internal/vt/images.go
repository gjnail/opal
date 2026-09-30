package vt

import (
	"image"
	"sync/atomic"
)

// Image is decoded pixel data, shared by every placement that shows it.
type Image struct {
	// ID and Number are the kitty graphics protocol's identifiers; images
	// from sixel and iTerm2 have neither.
	ID     uint32
	Number uint32
	Pix    *image.RGBA
	// Version is unique per image, for render caches.
	Version uint64
}

// Placement is an image shown on the grid. It belongs to the line its top
// row is on (Line.Images), so it scrolls, reflows and leaves history with
// that line.
type Placement struct {
	Image *Image
	ID    uint32 // kitty placement id
	Col   int
	// Cols and Rows is the cell area the image is scaled into.
	Cols, Rows int
	// Src is the part of the image shown, in image pixels.
	Src image.Rectangle
	// OffX and OffY shift the image within its first cell, in pixels.
	OffX, OffY int
	Z          int32
	// W and H are the drawn size in cells (fractional). Zero means fill
	// Cols x Rows. Sizes are kept in cells so images zoom with the text.
	W, H float32
}

var imageVersions atomic.Uint64

func newImage(pix *image.RGBA) *Image {
	return &Image{Pix: pix, Version: imageVersions.Add(1)}
}

// imageStore keeps kitty images by id, which programs can place again
// later, and the state of multi-part transfers.
type imageStore struct {
	byID     map[uint32]*Image
	byNumber map[uint32]*Image
	bytes    int
	order    []uint32 // ids, oldest first, for eviction

	kitty     *kittyTransfer
	iterm     *itermTransfer
	nextImage uint32
}

// imageQuota bounds the memory kitty images may keep around without
// placements, as kitty itself does.
const imageQuota = 320 << 20

func newImageStore() *imageStore {
	return &imageStore{byID: map[uint32]*Image{}, byNumber: map[uint32]*Image{}, nextImage: 1 << 31}
}

func (s *imageStore) add(img *Image) {
	if old := s.byID[img.ID]; old != nil {
		s.bytes -= len(old.Pix.Pix)
	} else {
		s.order = append(s.order, img.ID)
	}
	s.byID[img.ID] = img
	if img.Number != 0 {
		s.byNumber[img.Number] = img
	}
	s.bytes += len(img.Pix.Pix)
	for s.bytes > imageQuota && len(s.order) > 1 {
		id := s.order[0]
		s.order = s.order[1:]
		s.drop(id)
	}
}

func (s *imageStore) drop(id uint32) {
	img := s.byID[id]
	if img == nil {
		return
	}
	s.bytes -= len(img.Pix.Pix)
	delete(s.byID, id)
	if img.Number != 0 && s.byNumber[img.Number] == img {
		delete(s.byNumber, img.Number)
	}
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *imageStore) reset() {
	*s = *newImageStore()
}

// Hooks called by the screen code. Placements live on lines, so most
// events need no bookkeeping here.
func (s *imageStore) scrolled(n int)                                {}
func (s *imageStore) evicted(firstAbs int64)                        {}
func (s *imageStore) screenSwitched(alt bool)                       {}
func (s *imageStore) resized(t *Terminal)                           {}
func (s *imageStore) eraseCells(t *Terminal, l *Line, from, to int) {}

// place shows img at the cursor over cols x rows cells and moves the
// cursor past it unless keepCursor is set. Rows the image needs below the
// screen scroll it up, as sixel and iTerm2 images do.
func (t *Terminal) place(pl *Placement, keepCursor bool) {
	if pl.Cols <= 0 || pl.Rows <= 0 {
		return
	}
	pl.Col = t.cur.x
	t.cur.pendingWrap = false
	// Make room: scroll so the whole image fits on screen.
	if extra := t.cur.y + pl.Rows - t.rows; extra > 0 && !keepCursor {
		for i := 0; i < extra; i++ {
			t.scrollUp(t.top, t.bottom, 1)
		}
		t.cur.y = max(0, t.cur.y-extra)
	}
	l := t.line(t.cur.y)
	l.Images = append(l.Images, pl)
	t.seq++
	if keepCursor {
		return
	}
	// Leave the cursor on the image's last row, just past its right edge;
	// callers adjust from there to their protocol's convention.
	t.cur.y = min(t.rows-1, t.cur.y+pl.Rows-1)
	t.cur.x = min(t.cols-1, pl.Col+pl.Cols)
}

// cellsFor is how many cells w x h pixels cover.
func (t *Terminal) cellsFor(w, h int) (cols, rows int) {
	return max(1, (w+t.cellW-1)/t.cellW), max(1, (h+t.cellH-1)/t.cellH)
}

// VisibleImages calls fn for every placement whose area intersects the
// lines [from, from+n) of history+screen, with the line index its top
// row is on. The caller holds the lock.
func (t *Terminal) VisibleImages(from, n int, fn func(line int, pl *Placement)) {
	// Images anchored above the view can reach into it.
	const maxRowsAbove = 200
	start := max(0, from-maxRowsAbove)
	end := min(t.buf.count(), from+n)
	for i := start; i < end; i++ {
		l := t.buf.line(i)
		for _, pl := range l.Images {
			if i+pl.Rows > from {
				fn(i, pl)
			}
		}
	}
}

// deletePlacements removes placements matching pred from every line of
// the active screen and its history.
func (t *Terminal) deletePlacements(pred func(l *Line, row int, pl *Placement) bool) {
	for i := 0; i < t.buf.count(); i++ {
		l := t.buf.line(i)
		if len(l.Images) == 0 {
			continue
		}
		kept := l.Images[:0]
		for _, pl := range l.Images {
			if !pred(l, i, pl) {
				kept = append(kept, pl)
			}
		}
		clear(l.Images[len(kept):])
		l.Images = kept
		if len(kept) == 0 {
			l.Images = nil
		}
	}
	t.seq++
}

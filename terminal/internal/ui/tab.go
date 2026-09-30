package ui

import (
	"image"
	"math"
)

type splitDir uint8

const (
	splitLeaf splitDir = iota
	splitCols          // children side by side
	splitRows          // children stacked
)

// node is a pane (leaf) or a split of two nodes.
type node struct {
	pane   *Pane
	dir    splitDir
	a, b   *node
	ratio  float64 // a's share of the space
	parent *node
	rect   image.Rectangle
	// divider is the draggable gap between a and b.
	divider image.Rectangle
}

// Tab is a tree of panes.
type Tab struct {
	root  *node
	focus *Pane
	zoom  *Pane
	title string // set by the user; overrides the pane title
	id    int

	barRect  image.Rectangle
	closeTag int
}

func newTab(p *Pane) *Tab {
	return &Tab{root: &node{pane: p}, focus: p}
}

func (t *Tab) panes() []*Pane {
	var out []*Pane
	var walk func(n *node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		if n.dir == splitLeaf {
			out = append(out, n.pane)
			return
		}
		walk(n.a)
		walk(n.b)
	}
	walk(t.root)
	return out
}

func (t *Tab) find(p *Pane) *node {
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if n == nil || found != nil {
			return
		}
		if n.dir == splitLeaf && n.pane == p {
			found = n
			return
		}
		walk(n.a)
		walk(n.b)
	}
	walk(t.root)
	return found
}

// split puts np next to p: to the right (splitCols) or below (splitRows).
func (t *Tab) split(p, np *Pane, dir splitDir) {
	n := t.find(p)
	if n == nil {
		return
	}
	old := &node{pane: p, parent: n}
	nn := &node{pane: np, parent: n}
	n.pane = nil
	n.dir = dir
	n.a, n.b = old, nn
	n.ratio = 0.5
	t.focus = np
	t.zoom = nil
}

// remove takes p out of the tree and reports whether the tab is now empty.
func (t *Tab) remove(p *Pane) bool {
	n := t.find(p)
	if n == nil {
		return len(t.panes()) == 0
	}
	if t.zoom == p {
		t.zoom = nil
	}
	parent := n.parent
	if parent == nil {
		t.root = nil
		return true
	}
	sibling := parent.a
	if sibling == n {
		sibling = parent.b
	}
	// The sibling takes the parent's place.
	*parent = node{pane: sibling.pane, dir: sibling.dir, a: sibling.a, b: sibling.b, ratio: sibling.ratio, parent: parent.parent}
	if parent.a != nil {
		parent.a.parent = parent
		parent.b.parent = parent
	}
	if t.focus == p {
		t.focus = firstPane(parent)
	}
	return false
}

func firstPane(n *node) *Pane {
	for n != nil && n.dir != splitLeaf {
		n = n.a
	}
	if n == nil {
		return nil
	}
	return n.pane
}

// layout assigns rectangles, leaving gap pixels between split panes.
func (t *Tab) layout(r image.Rectangle, gap int) {
	if t.zoom != nil {
		for _, p := range t.panes() {
			p.rect = image.Rectangle{}
		}
		t.zoom.rect = r
		return
	}
	var walk func(n *node, r image.Rectangle)
	walk = func(n *node, r image.Rectangle) {
		n.rect = r
		if n.dir == splitLeaf {
			n.pane.rect = r
			return
		}
		if n.dir == splitCols {
			w := r.Dx() - gap
			wa := int(math.Round(float64(w) * n.ratio))
			n.divider = image.Rect(r.Min.X+wa, r.Min.Y, r.Min.X+wa+gap, r.Max.Y)
			walk(n.a, image.Rect(r.Min.X, r.Min.Y, r.Min.X+wa, r.Max.Y))
			walk(n.b, image.Rect(r.Min.X+wa+gap, r.Min.Y, r.Max.X, r.Max.Y))
		} else {
			h := r.Dy() - gap
			ha := int(math.Round(float64(h) * n.ratio))
			n.divider = image.Rect(r.Min.X, r.Min.Y+ha, r.Max.X, r.Min.Y+ha+gap)
			walk(n.a, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+ha))
			walk(n.b, image.Rect(r.Min.X, r.Min.Y+ha+gap, r.Max.X, r.Max.Y))
		}
	}
	if t.root != nil {
		walk(t.root, r)
	}
}

// splits lists the split nodes, for drawing and dragging dividers.
func (t *Tab) splits() []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		if n == nil || n.dir == splitLeaf {
			return
		}
		out = append(out, n)
		walk(n.a)
		walk(n.b)
	}
	walk(t.root)
	return out
}

// neighbor finds the pane next to p in a direction, preferring the one
// that overlaps it most.
func (t *Tab) neighbor(p *Pane, dx, dy int) *Pane {
	r := p.rect
	var best *Pane
	bestScore := -1
	for _, q := range t.panes() {
		if q == p {
			continue
		}
		o := q.rect
		var adjacent bool
		var overlap int
		switch {
		case dx > 0:
			adjacent = o.Min.X >= r.Max.X
			overlap = min(r.Max.Y, o.Max.Y) - max(r.Min.Y, o.Min.Y)
		case dx < 0:
			adjacent = o.Max.X <= r.Min.X
			overlap = min(r.Max.Y, o.Max.Y) - max(r.Min.Y, o.Min.Y)
		case dy > 0:
			adjacent = o.Min.Y >= r.Max.Y
			overlap = min(r.Max.X, o.Max.X) - max(r.Min.X, o.Min.X)
		case dy < 0:
			adjacent = o.Max.Y <= r.Min.Y
			overlap = min(r.Max.X, o.Max.X) - max(r.Min.X, o.Min.X)
		}
		if !adjacent || overlap <= 0 {
			continue
		}
		dist := abs(o.Min.X-r.Max.X) + abs(r.Min.X-o.Max.X) + abs(o.Min.Y-r.Max.Y) + abs(r.Min.Y-o.Max.Y)
		score := overlap*4 - dist
		if score > bestScore {
			best, bestScore = q, score
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// resize grows or shrinks p toward a direction by moving the nearest
// matching divider.
func (t *Tab) resize(p *Pane, dx, dy int, step float64) {
	n := t.find(p)
	for n != nil && n.parent != nil {
		par := n.parent
		if (dx != 0 && par.dir == splitCols) || (dy != 0 && par.dir == splitRows) {
			delta := step * float64(dx+dy)
			par.ratio = math.Min(0.9, math.Max(0.1, par.ratio+delta))
			return
		}
		n = par
	}
}

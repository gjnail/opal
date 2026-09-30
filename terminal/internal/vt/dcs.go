package vt

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// dcsHandler receives the payload of a DCS string.
type dcsHandler interface {
	put(b []byte)
	unhook(t *Terminal)
}

// bufferedDCS collects a small payload and handles it at the end.
type bufferedDCS struct {
	data []byte
	max  int
	done func(t *Terminal, data []byte)
}

func (d *bufferedDCS) put(b []byte) {
	if len(d.data)+len(b) <= d.max {
		d.data = append(d.data, b...)
	}
}

func (d *bufferedDCS) unhook(t *Terminal) { d.done(t, d.data) }

func (t *Terminal) dcsHook(p *params, private byte, inter []byte, final byte) {
	t.lastValid = false
	t.dcs = nil
	switch {
	case final == 'q' && len(inter) == 1 && inter[0] == '$':
		t.dcs = &bufferedDCS{max: 64, done: (*Terminal).decrqss}
	case final == 'q' && len(inter) == 1 && inter[0] == '+':
		t.dcs = &bufferedDCS{max: 4096, done: (*Terminal).xtgettcap}
	case final == 'q' && len(inter) == 0 && private == 0:
		t.dcs = t.newSixel(p)
	}
}

func (t *Terminal) dcsPut(b []byte) {
	if t.dcs != nil {
		t.dcs.put(b)
	}
}

func (t *Terminal) dcsUnhook() {
	if t.dcs != nil {
		t.dcs.unhook(t)
		t.dcs = nil
	}
}

// decrqss answers DECRQSS (request status string).
func (t *Terminal) decrqss(data []byte) {
	var v string
	switch string(data) {
	case "m":
		v = t.cur.pen.sgrString() + "m"
	case "r":
		v = fmt.Sprintf("%d;%dr", t.top+1, t.bottom+1)
	case "s":
		v = fmt.Sprintf("%d;%ds", t.left+1, t.right+1)
	case " q":
		v = fmt.Sprintf("%d q", int(t.cursorStyle))
	case "\"q":
		if t.cur.pen.attrs&AttrProtected != 0 {
			v = "1\"q"
		} else {
			v = "0\"q"
		}
	case "\"p":
		v = "65;1\"p"
	case "t", "*|":
		v = fmt.Sprintf("%d%s", t.rows, string(data))
	default:
		t.reply("\x1bP0$r\x1b\\")
		return
	}
	t.reply("\x1bP1$r" + v + "\x1b\\")
}

// termcaps are the capabilities XTGETTCAP can report. Programs like
// Neovim and tmux use this to discover truecolor, styled underlines, the
// clipboard and synchronized output without trusting $TERM.
var termcaps = map[string]string{
	"TN":     "xterm-256color",
	"name":   "xterm-256color",
	"Co":     "256",
	"colors": "256",
	"RGB":    "8/8/8",
	"Tc":     "",
	"bce":    "",
	"Smulx":  `\E[4:%p1%dm`,
	"Setulc": `\E[58:2::%p1%{65536}%/%d:%p1%{256}%/%{255}%&%d:%p1%{255}%&%d%;m`,
	"Ss":     `\E[%p1%d q`,
	"Se":     `\E[0 q`,
	"Ms":     `\E]52;%p1%s;%p2%s\E\\`,
	"Sync":   `\E[?2026%?%p1%{1}%-%tl%eh%;`,
	"Enbp":   `\E[?2004h`,
	"Dsbp":   `\E[?2004l`,
	"fsl":    `^G`,
	"tsl":    `\E]2;`,
	"sitm":   `\E[3m`,
	"ritm":   `\E[23m`,
	"smxx":   `\E[9m`,
	"rmxx":   `\E[29m`,
	"Smol":   `\E[53m`,
	"kbs":    `^?`,
	"hpa":    `\E[%i%p1%dG`,
	"XM":     `\E[?1006;1000%?%p1%{1}%=%th%el%;`,
	"xm":     `\E[<%i%p3%d;%p1%d;%p2%d;%?%p4%tM%em%;`,
}

func (t *Terminal) xtgettcap(data []byte) {
	for _, h := range strings.Split(string(data), ";") {
		name, err := hex.DecodeString(h)
		if err != nil {
			t.reply("\x1bP0+r" + h + "\x1b\\")
			continue
		}
		v, ok := termcaps[string(name)]
		if !ok {
			t.reply("\x1bP0+r" + h + "\x1b\\")
			continue
		}
		if v == "" && string(name) != "TN" {
			t.reply("\x1bP1+r" + h + "\x1b\\")
			continue
		}
		t.reply("\x1bP1+r" + h + "=" + hex.EncodeToString([]byte(v)) + "\x1b\\")
	}
}

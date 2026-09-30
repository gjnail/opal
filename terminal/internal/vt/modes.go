package vt

type mode uint8

const (
	modeCursorKeys         mode = iota // ?1 DECCKM
	modeColumn132                      // ?3 DECCOLM (accepted, not acted on)
	modeSmoothScroll                   // ?4 DECSCLM
	modeReverseVideo                   // ?5 DECSCNM
	modeOrigin                         // ?6 DECOM
	modeAutowrap                       // ?7 DECAWM
	modeAutoRepeat                     // ?8 DECARM
	modeMouseX10                       // ?9
	modeCursorBlink                    // ?12
	modeCursorVisible                  // ?25 DECTCEM
	modeAllow132                       // ?40
	modeReverseWrap                    // ?45
	modeAltScreenLegacy                // ?47
	modeKeypadApp                      // ?66 DECNKM, also ESC = / ESC >
	modeBackarrowBS                    // ?67 DECBKM: backspace sends BS, not DEL
	modeLRMargins                      // ?69 DECLRMM
	modeSixelDisplay                   // ?80 DECSDM: set means sixel scrolling is off
	modeMouseNormal                    // ?1000
	modeMouseHighlight                 // ?1001
	modeMouseButton                    // ?1002
	modeMouseAny                       // ?1003
	modeFocusEvents                    // ?1004
	modeMouseUTF8                      // ?1005
	modeMouseSGR                       // ?1006
	modeAltScroll                      // ?1007
	modeMouseURXVT                     // ?1015
	modeMouseSGRPixels                 // ?1016
	modeMetaSendsEsc                   // ?1036
	modeAltSendsEsc                    // ?1039
	modeBellUrgent                     // ?1042
	modeAltScreen                      // ?1047
	modeSaveCursor                     // ?1048
	modeAltScreenSave                  // ?1049
	modeSixelPrivateColors             // ?1070
	modeBracketedPaste                 // ?2004
	modeSyncOutput                     // ?2026
	modeGraphemeCluster                // ?2027
	modeColorSchemeUpdates             // ?2031
	modeInBandResize                   // ?2048
	modeSixelCursorRight               // ?8452
	modeKeyboardLocked                 // 2 KAM (ANSI)
	modeInsert                         // 4 IRM (ANSI)
	modeSendReceive                    // 12 SRM (ANSI)
	modeLineFeedNewLine                // 20 LNM (ANSI)
	modeCount
)

var decModes = map[int]mode{
	1: modeCursorKeys, 3: modeColumn132, 4: modeSmoothScroll, 5: modeReverseVideo,
	6: modeOrigin, 7: modeAutowrap, 8: modeAutoRepeat, 9: modeMouseX10, 12: modeCursorBlink,
	25: modeCursorVisible, 40: modeAllow132, 45: modeReverseWrap, 47: modeAltScreenLegacy,
	66: modeKeypadApp, 67: modeBackarrowBS, 69: modeLRMargins, 80: modeSixelDisplay,
	1000: modeMouseNormal, 1001: modeMouseHighlight, 1002: modeMouseButton, 1003: modeMouseAny,
	1004: modeFocusEvents, 1005: modeMouseUTF8, 1006: modeMouseSGR, 1007: modeAltScroll,
	1015: modeMouseURXVT, 1016: modeMouseSGRPixels, 1036: modeMetaSendsEsc, 1039: modeAltSendsEsc,
	1042: modeBellUrgent, 1047: modeAltScreen, 1048: modeSaveCursor, 1049: modeAltScreenSave,
	1070: modeSixelPrivateColors, 2004: modeBracketedPaste, 2026: modeSyncOutput,
	2027: modeGraphemeCluster, 2031: modeColorSchemeUpdates, 2048: modeInBandResize,
	8452: modeSixelCursorRight,
}

var ansiModes = map[int]mode{
	2: modeKeyboardLocked, 4: modeInsert, 12: modeSendReceive, 20: modeLineFeedNewLine,
}

type modes struct{ bits uint64 }

func (m *modes) get(md mode) bool { return m.bits&(1<<md) != 0 }

func (m *modes) set(md mode, on bool) {
	if on {
		m.bits |= 1 << md
	} else {
		m.bits &^= 1 << md
	}
}

func defaultModes() modes {
	var m modes
	m.set(modeAutowrap, true)
	m.set(modeAutoRepeat, true)
	m.set(modeCursorVisible, true)
	m.set(modeSendReceive, true)
	m.set(modeSixelPrivateColors, true)
	return m
}

var mouseTrackingModes = []mode{modeMouseX10, modeMouseNormal, modeMouseHighlight, modeMouseButton, modeMouseAny}
var mouseEncodingModes = []mode{modeMouseUTF8, modeMouseSGR, modeMouseURXVT, modeMouseSGRPixels}

// setMode applies DECSET/DECRST (dec) or SM/RM.
func (t *Terminal) setMode(n int, dec, on bool) {
	var md mode
	var ok bool
	if dec {
		md, ok = decModes[n]
	} else {
		md, ok = ansiModes[n]
	}
	if !ok {
		return
	}
	// Mouse tracking and mouse encodings are each one setting spread over
	// several mode numbers: turning one on turns the others off.
	for _, group := range [][]mode{mouseTrackingModes, mouseEncodingModes} {
		for _, g := range group {
			if g == md && on {
				for _, o := range group {
					t.modes.set(o, false)
				}
			}
		}
	}

	switch md {
	case modeOrigin:
		t.modes.set(md, on)
		t.cur.origin = on
		t.moveTo(0, 0)
		return
	case modeLRMargins:
		t.modes.set(md, on)
		if !on {
			t.left, t.right = 0, t.cols-1
		}
		return
	case modeReverseVideo:
		if t.modes.get(md) != on {
			t.seq++
		}
	case modeAltScreenLegacy, modeAltScreen:
		t.modes.set(md, on)
		t.switchScreen(on, md == modeAltScreen, false)
		return
	case modeAltScreenSave:
		t.modes.set(md, on)
		if on {
			t.saveCursor()
			t.switchScreen(true, true, true)
		} else {
			t.switchScreen(false, false, false)
			t.restoreCursor()
		}
		return
	case modeSaveCursor:
		if on {
			t.saveCursor()
		} else {
			t.restoreCursor()
		}
		return
	case modeColumn132:
		// xterm clears the screen and homes the cursor when DECCOLM is
		// allowed. We don't resize the window, but vttest expects the rest.
		if t.modes.get(modeAllow132) {
			t.modes.set(md, on)
			t.top, t.bottom = 0, t.rows-1
			t.left, t.right = 0, t.cols-1
			t.eraseDisplay(2, false)
			t.moveTo(0, 0)
		}
		return
	case modeSyncOutput:
		if on && !t.modes.get(md) {
			t.syncStart = nowFunc()
		}
	case modeInBandResize:
		t.modes.set(md, on)
		if on {
			t.reportInBandSize()
		}
		return
	case modeColorSchemeUpdates:
		t.modes.set(md, on)
		return
	}
	t.modes.set(md, on)
}

// modeReport answers DECRQM: 0 unknown, 1 set, 2 reset, 3 permanently
// set, 4 permanently reset.
func (t *Terminal) modeReport(n int, dec bool) int {
	var md mode
	var ok bool
	if dec {
		md, ok = decModes[n]
	} else {
		md, ok = ansiModes[n]
	}
	if !ok {
		return 0
	}
	switch md {
	case modeKeyboardLocked:
		return 4
	case modeSaveCursor:
		return 2
	}
	if t.modes.get(md) {
		return 1
	}
	return 2
}

// MouseTracking says which mouse events the program wants.
type MouseTracking int

const (
	MouseTrackNone   MouseTracking = iota
	MouseTrackX10                  // presses only
	MouseTrackNormal               // presses and releases
	MouseTrackButton               // plus motion while a button is held
	MouseTrackAny                  // plus all motion
)

// MouseMode returns the active tracking mode.
func (t *Terminal) MouseMode() MouseTracking {
	switch {
	case t.modes.get(modeMouseAny):
		return MouseTrackAny
	case t.modes.get(modeMouseButton):
		return MouseTrackButton
	case t.modes.get(modeMouseNormal), t.modes.get(modeMouseHighlight):
		return MouseTrackNormal
	case t.modes.get(modeMouseX10):
		return MouseTrackX10
	}
	return MouseTrackNone
}

// BracketedPaste reports mode 2004.
func (t *Terminal) BracketedPaste() bool { return t.modes.get(modeBracketedPaste) }

// FocusReporting reports mode 1004.
func (t *Terminal) FocusReporting() bool { return t.modes.get(modeFocusEvents) }

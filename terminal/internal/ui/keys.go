package ui

import (
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/key"

	"opal/terminal/internal/vt"
)

// keyNames maps Gio key names to the names used in keybindings.
var keyNames = map[key.Name]string{
	key.NameLeftArrow: "left", key.NameRightArrow: "right", key.NameUpArrow: "up", key.NameDownArrow: "down",
	key.NameReturn: "enter", key.NameEnter: "enter", key.NameEscape: "escape",
	key.NameHome: "home", key.NameEnd: "end", key.NameDeleteBackward: "backspace", key.NameDeleteForward: "delete",
	key.NamePageUp: "pageup", key.NamePageDown: "pagedown", key.NameTab: "tab", key.NameSpace: "space",
	"+": "plus", "=": "plus", "-": "minus", ",": "comma", ".": "period", "/": "slash", "\\": "backslash",
	"`": "backtick", ";": "semicolon", "'": "quote", "[": "bracketleft", "]": "bracketright", "*": "asterisk",
}

// normalizeKeyName maps a key name from a config file to our form.
func normalizeKeyName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "return":
		return "enter"
	case "esc":
		return "escape"
	case "del":
		return "delete"
	case "pgup":
		return "pageup"
	case "pgdn", "pgdown":
		return "pagedown"
	case "equal", "equals", "=", "+":
		return "plus"
	case "-", "dash":
		return "minus"
	case ",":
		return "comma"
	case ".":
		return "period"
	case "/":
		return "slash"
	case "[":
		return "bracketleft"
	case "]":
		return "bracketright"
	}
	return s
}

// chord renders a key event as "ctrl+alt+shift+super+key".
func chord(mods key.Modifiers, name key.Name) string {
	n, ok := keyNames[name]
	if !ok {
		n = strings.ToLower(string(name))
	}
	var parts []string
	if mods.Contain(key.ModCtrl) {
		parts = append(parts, "ctrl")
	}
	if mods.Contain(key.ModAlt) {
		parts = append(parts, "alt")
	}
	if mods.Contain(key.ModShift) {
		parts = append(parts, "shift")
	}
	if mods.Contain(key.ModSuper) || mods.Contain(key.ModCommand) {
		parts = append(parts, "super")
	}
	return strings.Join(append(parts, n), "+")
}

// normalizeChord puts a user-written chord ("Ctrl+Shift+T", "cmd+t") in
// canonical order.
func normalizeChord(s string) string {
	parts := strings.Split(s, "+")
	// "ctrl++" means ctrl and the plus key.
	if strings.HasSuffix(s, "++") {
		parts = append(strings.Split(strings.TrimSuffix(s, "++"), "+"), "plus")
	}
	var ctrl, alt, shift, super bool
	keyName := ""
	for _, p := range parts {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			ctrl = true
		case "alt", "option", "opt":
			alt = true
		case "shift":
			shift = true
		case "super", "cmd", "command", "meta", "win", "logo":
			super = true
		case "":
		default:
			keyName = normalizeKeyName(p)
		}
	}
	var out []string
	if ctrl {
		out = append(out, "ctrl")
	}
	if alt {
		out = append(out, "alt")
	}
	if shift {
		out = append(out, "shift")
	}
	if super {
		out = append(out, "super")
	}
	return strings.Join(append(out, keyName), "+")
}

// defaultKeys are the built-in bindings. macOS uses Cmd for everything
// that would otherwise need Ctrl+Shift, like other Mac terminals.
func defaultKeys() map[string]string {
	if runtime.GOOS == "darwin" {
		k := map[string]string{
			"super+t": "new_tab", "super+w": "close_pane", "super+n": "new_window",
			"super+shift+bracketright": "next_tab", "super+shift+bracketleft": "prev_tab",
			"ctrl+tab": "next_tab", "ctrl+shift+tab": "prev_tab",
			"super+d": "split_right", "super+shift+d": "split_down",
			"super+alt+left": "focus_left", "super+alt+right": "focus_right", "super+alt+up": "focus_up", "super+alt+down": "focus_down",
			"super+ctrl+left": "resize_left", "super+ctrl+right": "resize_right", "super+ctrl+up": "resize_up", "super+ctrl+down": "resize_down",
			"super+shift+enter": "toggle_zoom",
			"super+c":           "copy", "super+v": "paste", "super+a": "select_all",
			"super+f": "find", "super+shift+p": "command_palette", "super+shift+space": "quick_select",
			"super+shift+h": "history_search", "super+shift+j": "recent_dirs",
			"super+plus": "font_bigger", "super+minus": "font_smaller", "super+0": "font_reset",
			"super+up": "prev_prompt", "super+down": "next_prompt",
			"super+shift+up": "scroll_up", "super+shift+down": "scroll_down",
			"shift+pageup": "scroll_page_up", "shift+pagedown": "scroll_page_down",
			"super+home": "scroll_top", "super+end": "scroll_bottom",
			"super+k": "clear_scrollback", "super+ctrl+f": "toggle_fullscreen", "super+enter": "toggle_fullscreen",
			"super+comma": "open_config", "super+shift+i": "toggle_broadcast", "super+shift+r": "rename_tab",
		}
		for i := 1; i <= 9; i++ {
			k["super+"+string(rune('0'+i))] = "goto_tab_" + string(rune('0'+i))
		}
		return k
	}
	k := map[string]string{
		"ctrl+shift+t": "new_tab", "ctrl+shift+w": "close_pane", "ctrl+shift+n": "new_window",
		"ctrl+tab": "next_tab", "ctrl+shift+tab": "prev_tab",
		"ctrl+pagedown": "next_tab", "ctrl+pageup": "prev_tab",
		"alt+shift+plus": "split_right", "alt+shift+minus": "split_down", "alt+shift+d": "split_auto",
		"alt+left": "focus_left", "alt+right": "focus_right", "alt+up": "focus_up", "alt+down": "focus_down",
		"alt+shift+left": "resize_left", "alt+shift+right": "resize_right", "alt+shift+up": "resize_up", "alt+shift+down": "resize_down",
		"ctrl+shift+z": "toggle_zoom",
		"ctrl+shift+c": "copy", "ctrl+shift+v": "paste", "ctrl+shift+a": "select_all",
		"ctrl+shift+f": "find", "ctrl+shift+p": "command_palette", "ctrl+shift+space": "quick_select",
		"ctrl+shift+h": "history_search", "ctrl+shift+j": "recent_dirs",
		"ctrl+plus": "font_bigger", "ctrl+minus": "font_smaller", "ctrl+0": "font_reset",
		"ctrl+up": "prev_prompt", "ctrl+down": "next_prompt",
		"ctrl+shift+up": "scroll_up", "ctrl+shift+down": "scroll_down",
		"shift+pageup": "scroll_page_up", "shift+pagedown": "scroll_page_down",
		"ctrl+shift+home": "scroll_top", "ctrl+shift+end": "scroll_bottom",
		"ctrl+shift+k": "clear_scrollback", "f11": "toggle_fullscreen",
		"ctrl+comma": "open_config", "ctrl+shift+i": "toggle_broadcast", "ctrl+shift+r": "rename_tab",
		"ctrl+shift+e": "select_last_output",
	}
	if runtime.GOOS == "windows" {
		// Windows Terminal's habits: Alt+Enter for full screen, Ctrl+V
		// pastes. Ctrl+C copies only when there's a selection (handled
		// in the key path) so it still interrupts programs.
		k["alt+enter"] = "toggle_fullscreen"
		k["ctrl+v"] = "paste"
	}
	for i := 1; i <= 9; i++ {
		k["ctrl+alt+"+string(rune('0'+i))] = "goto_tab_" + string(rune('0'+i))
	}
	return k
}

// keymap merges user bindings over the defaults. A binding to "none"
// (or "") removes a default.
func keymap(user map[string]string) map[string]string {
	k := defaultKeys()
	for c, a := range user {
		c = normalizeChord(c)
		if a == "" || a == "none" || a == "unbind" {
			delete(k, c)
			continue
		}
		k[c] = a
	}
	return k
}

// keysFor lists the chords bound to an action, for the command palette.
func keysFor(km map[string]string, action string) []string {
	var out []string
	for c, a := range km {
		if a == action {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) < len(out[j]) })
	return out
}

var namedKeys = map[key.Name]vt.Key{
	key.NameReturn: vt.KeyEnter, key.NameEnter: vt.KeyKPEnter, key.NameTab: vt.KeyTab,
	key.NameDeleteBackward: vt.KeyBackspace, key.NameEscape: vt.KeyEscape,
	key.NameUpArrow: vt.KeyUp, key.NameDownArrow: vt.KeyDown, key.NameLeftArrow: vt.KeyLeft, key.NameRightArrow: vt.KeyRight,
	key.NameHome: vt.KeyHome, key.NameEnd: vt.KeyEnd, key.NameDeleteForward: vt.KeyDelete,
	key.NamePageUp: vt.KeyPageUp, key.NamePageDown: vt.KeyPageDown,
	key.NameF1: vt.KeyF1, key.NameF2: vt.KeyF2, key.NameF3: vt.KeyF3, key.NameF4: vt.KeyF4,
	key.NameF5: vt.KeyF5, key.NameF6: vt.KeyF6, key.NameF7: vt.KeyF7, key.NameF8: vt.KeyF8,
	key.NameF9: vt.KeyF9, key.NameF10: vt.KeyF10, key.NameF11: vt.KeyF11, key.NameF12: vt.KeyF12,
}

// toVTKey converts a Gio key event. ok is false when the key is plain
// text that will arrive as an EditEvent instead (so it isn't sent twice).
func toVTKey(e key.Event, kittyAll bool) (vt.KeyEvent, bool) {
	mods := toVTMods(e.Modifiers)
	action := vt.KeyPress
	if e.State == key.Release {
		action = vt.KeyRelease
	}
	if k, ok := namedKeys[e.Name]; ok {
		return vt.KeyEvent{Key: k, Mods: mods, Action: action}, true
	}
	if e.Name == key.NameSpace {
		if mods&(vt.ModCtrl|vt.ModAlt|vt.ModSuper) == 0 && !kittyAll {
			return vt.KeyEvent{}, false
		}
		return vt.KeyEvent{Key: vt.KeyChar, Rune: ' ', Text: " ", Mods: mods, Action: action}, true
	}
	name := string(e.Name)
	if utf8.RuneCountInString(name) != 1 {
		return vt.KeyEvent{}, false // modifier keys and the like
	}
	r, _ := utf8.DecodeRuneInString(name)
	base := unicode.ToLower(r)
	text := string(base)
	if mods&vt.ModShift != 0 {
		text = string(unicode.ToUpper(r))
	}
	// Plain and shifted characters come through EditEvent, which knows
	// the keyboard layout; only modified keys are handled here.
	if mods&(vt.ModCtrl|vt.ModAlt|vt.ModSuper) == 0 && !kittyAll {
		return vt.KeyEvent{}, false
	}
	return vt.KeyEvent{Key: vt.KeyChar, Rune: base, Text: text, Mods: mods, Action: action}, true
}

// Package settings reads Opal Terminal's configuration: the [terminal]
// table of Opal's config.toml, plus the theme and background the prompt
// already uses, so the terminal and the prompt always match.
package settings

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"opal/internal/config"
)

// Config is the terminal's configuration after defaults are applied.
type Config struct {
	FontFamily   []string          `toml:"font_family"`
	FontSize     float64           `toml:"font_size"` // points
	LineHeight   float64           `toml:"line_height"`
	CellWidth    float64           `toml:"cell_width"`
	Shell        string            `toml:"shell"` // a command line, or a profile name
	Scrollback   int               `toml:"scrollback"`
	CursorStyle  string            `toml:"cursor_style"` // block, bar, underline
	CursorBlink  bool              `toml:"cursor_blink"`
	CopyOnSelect bool              `toml:"copy_on_select"`
	BoldIsBright bool              `toml:"bold_is_bright"`
	MinContrast  float64           `toml:"min_contrast"`
	Opacity      float64           `toml:"opacity"`
	Padding      int               `toml:"padding"` // pixels at 100% scale
	ConfirmClose bool              `toml:"confirm_close"`
	Graphemes    bool              `toml:"grapheme_clustering"`
	Bell         string            `toml:"bell"` // visual, sound, none
	NotifyAfter  float64           `toml:"notify_after"`
	Notify       string            `toml:"notifications"` // unfocused, always, never
	Restore      bool              `toml:"restore_session"`
	WordChars    string            `toml:"word_chars"`
	Theme        string            `toml:"theme"` // overrides the prompt's theme
	Background   string            `toml:"background"`
	Colors       Colors            `toml:"colors"`
	Profiles     []Profile         `toml:"profiles"`
	Keys         map[string]string `toml:"keys"`
	Clipboard    ClipboardPolicy   `toml:"clipboard"`

	File     string   `toml:"-"`
	Warnings []string `toml:"-"`
}

// Colors override theme-derived colors. Any field left empty keeps the
// theme's color.
type Colors struct {
	Foreground string   `toml:"foreground"`
	Background string   `toml:"background"`
	Cursor     string   `toml:"cursor"`
	CursorText string   `toml:"cursor_text"`
	Selection  string   `toml:"selection"`
	Palette    []string `toml:"palette"` // up to 16 entries
}

// ClipboardPolicy says what programs may do with the clipboard via
// OSC 52. Reading is off by default: a program you ssh into could
// otherwise read whatever you last copied.
type ClipboardPolicy struct {
	Write bool `toml:"write"`
	Read  bool `toml:"read"`
}

// Profile is a shell (or any program) a tab can run.
type Profile struct {
	Name    string            `toml:"name"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Cwd     string            `toml:"cwd"`
	Env     map[string]string `toml:"env"`
	// Login starts the shell as a login shell (argv[0] "-zsh").
	Login bool `toml:"login"`
}

// Defaults returns the built-in configuration.
func Defaults() *Config {
	size := 12.0
	if runtime.GOOS == "darwin" {
		size = 13
	}
	return &Config{
		FontSize:     size,
		LineHeight:   1,
		CellWidth:    1,
		Scrollback:   10000,
		CursorStyle:  "block",
		CursorBlink:  true,
		MinContrast:  1,
		Opacity:      1,
		Padding:      8,
		ConfirmClose: true,
		Graphemes:    true,
		Bell:         "visual",
		NotifyAfter:  10,
		Notify:       "unfocused",
		Restore:      true,
		WordChars:    "-_./~:@+%#?&=",
		Clipboard:    ClipboardPolicy{Write: true},
	}
}

// Load reads config.toml. Problems are reported as warnings and never
// stop the terminal from starting.
func Load() *Config {
	c := Defaults()
	c.File = config.Path()
	oc, err := config.Load()
	if err != nil {
		c.Warnings = append(c.Warnings, err.Error())
	}
	c.Theme, c.Background = oc.Theme, oc.Background

	b, err := os.ReadFile(c.File)
	if err != nil {
		return c
	}
	// Decode only our table; the rest of the file belongs to the CLI.
	var file struct {
		Terminal toml.Primitive `toml:"terminal"`
	}
	md, err := toml.Decode(string(b), &file)
	if err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("%s: %v", c.File, err))
		return c
	}
	if !md.IsDefined("terminal") {
		return c
	}
	var raw rawConfig
	if err := md.PrimitiveDecode(file.Terminal, &raw); err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("%s [terminal]: %v", c.File, err))
		return c
	}
	raw.apply(c)
	c.normalize()
	return c
}

// rawConfig mirrors [terminal] with pointers, so we can tell "unset" from
// "set to the zero value".
type rawConfig struct {
	FontFamily   stringList        `toml:"font_family"`
	FontSize     *float64          `toml:"font_size"`
	LineHeight   *float64          `toml:"line_height"`
	CellWidth    *float64          `toml:"cell_width"`
	Shell        *string           `toml:"shell"`
	Scrollback   *int              `toml:"scrollback"`
	CursorStyle  *string           `toml:"cursor_style"`
	CursorBlink  *bool             `toml:"cursor_blink"`
	CopyOnSelect *bool             `toml:"copy_on_select"`
	BoldIsBright *bool             `toml:"bold_is_bright"`
	MinContrast  *float64          `toml:"min_contrast"`
	Opacity      *float64          `toml:"opacity"`
	Padding      *int              `toml:"padding"`
	ConfirmClose *bool             `toml:"confirm_close"`
	Graphemes    *bool             `toml:"grapheme_clustering"`
	Bell         *string           `toml:"bell"`
	NotifyAfter  *float64          `toml:"notify_after"`
	Notify       *string           `toml:"notifications"`
	Restore      *bool             `toml:"restore_session"`
	WordChars    *string           `toml:"word_chars"`
	Theme        *string           `toml:"theme"`
	Background   *string           `toml:"background"`
	Colors       Colors            `toml:"colors"`
	Profiles     []Profile         `toml:"profiles"`
	Keys         map[string]string `toml:"keys"`
	Clipboard    *ClipboardPolicy  `toml:"clipboard"`
}

func (r *rawConfig) apply(c *Config) {
	if len(r.FontFamily) > 0 {
		c.FontFamily = r.FontFamily
	}
	setF := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	setS := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setB := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setF(&c.FontSize, r.FontSize)
	setF(&c.LineHeight, r.LineHeight)
	setF(&c.CellWidth, r.CellWidth)
	setS(&c.Shell, r.Shell)
	if r.Scrollback != nil {
		c.Scrollback = *r.Scrollback
	}
	setS(&c.CursorStyle, r.CursorStyle)
	setB(&c.CursorBlink, r.CursorBlink)
	setB(&c.CopyOnSelect, r.CopyOnSelect)
	setB(&c.BoldIsBright, r.BoldIsBright)
	setF(&c.MinContrast, r.MinContrast)
	setF(&c.Opacity, r.Opacity)
	if r.Padding != nil {
		c.Padding = *r.Padding
	}
	setB(&c.ConfirmClose, r.ConfirmClose)
	setB(&c.Graphemes, r.Graphemes)
	setS(&c.Bell, r.Bell)
	setF(&c.NotifyAfter, r.NotifyAfter)
	setS(&c.Notify, r.Notify)
	setB(&c.Restore, r.Restore)
	setS(&c.WordChars, r.WordChars)
	setS(&c.Theme, r.Theme)
	setS(&c.Background, r.Background)
	c.Colors = r.Colors
	c.Profiles = r.Profiles
	c.Keys = r.Keys
	if r.Clipboard != nil {
		c.Clipboard = *r.Clipboard
	}
}

func (c *Config) warnf(format string, args ...any) {
	c.Warnings = append(c.Warnings, fmt.Sprintf(format, args...))
}

// normalize clamps values to something the terminal can use.
func (c *Config) normalize() {
	if c.FontSize < 4 || c.FontSize > 200 {
		c.warnf("font_size %v is out of range; using 12", c.FontSize)
		c.FontSize = 12
	}
	if c.LineHeight < 0.5 || c.LineHeight > 3 {
		c.LineHeight = 1
	}
	if c.CellWidth < 0.5 || c.CellWidth > 3 {
		c.CellWidth = 1
	}
	if c.Scrollback < 0 {
		c.Scrollback = 0
	}
	if c.Scrollback > 1_000_000 {
		c.Scrollback = 1_000_000
	}
	switch c.CursorStyle {
	case "block", "bar", "underline":
	default:
		c.warnf("cursor_style %q: use block, bar or underline", c.CursorStyle)
		c.CursorStyle = "block"
	}
	if c.Opacity <= 0 || c.Opacity > 1 {
		c.Opacity = 1
	}
	if c.Padding < 0 || c.Padding > 100 {
		c.Padding = 8
	}
	switch c.Bell {
	case "visual", "sound", "none":
	default:
		c.Bell = "visual"
	}
	switch c.Notify {
	case "unfocused", "always", "never":
	default:
		c.warnf("notifications %q: use unfocused, always or never", c.Notify)
		c.Notify = "unfocused"
	}
	c.Background = strings.ToLower(c.Background)
}

// stringList accepts either "a" or ["a", "b"].
type stringList []string

func (s *stringList) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		*s = []string{v}
	case []any:
		for _, e := range v {
			str, ok := e.(string)
			if !ok {
				return fmt.Errorf("expected strings, got %T", e)
			}
			*s = append(*s, str)
		}
	default:
		return fmt.Errorf("expected a string or a list of strings, got %T", v)
	}
	return nil
}

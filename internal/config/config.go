// Package config loads ~/.config/opal/config.toml, the one config file
// shared by every shell.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"opal/internal/assets"
	"opal/internal/platform"
)

// Config mirrors config.toml.
type Config struct {
	Theme       string              `toml:"theme"`
	Icons       string              `toml:"icons"`
	Color       string              `toml:"color"`
	Background  string              `toml:"background"`
	Greeting    bool                `toml:"greeting"`
	Suggest     bool                `toml:"suggestions"`
	Completions bool                `toml:"completions"` // load completion scripts that installed tools generate
	Plugins     []string            `toml:"plugins"`
	Path        []string            `toml:"path"`
	Env         map[string]Variants `toml:"env"`
	Aliases     map[string]Variants `toml:"aliases"`
	Functions   map[string]Variants `toml:"functions"`
	Prompt      Prompt              `toml:"prompt"`
	Core        Core                `toml:"core"`
	Shell       Shells              `toml:"shell"`
	Pkg         Pkg                 `toml:"pkg"`
	History     History             `toml:"history"`
	Update      Update              `toml:"update"`

	File   string `toml:"-"` // where it was (or would be) loaded from
	Loaded bool   `toml:"-"` // false when running on defaults
}

type Prompt struct {
	BlankLine         bool    `toml:"blank_line"`
	DurationThreshold float64 `toml:"duration_threshold"`
	GitTimeoutMS      int     `toml:"git_timeout_ms"`
	MaxDirDepth       int     `toml:"max_dir_depth"`
	Time              bool    `toml:"time"`
	Transient         bool    `toml:"transient"`      // collapse finished prompts to ❯
	SemanticMarks     bool    `toml:"semantic_marks"` // OSC 133 / OSC 7 for terminals
}

// History is the shared, cross-shell command history behind Ctrl+R.
type History struct {
	Shared bool `toml:"shared"` // record commands from every shell into one history
	CtrlR  bool `toml:"ctrl_r"` // bind Ctrl+R to opal's fuzzy history picker
}

// Update says where `opal update` looks for new releases.
type Update struct {
	Repo string `toml:"repo"` // GitHub "owner/name"
}

type Core struct {
	Defaults bool `toml:"defaults"`
}

type Shells struct {
	Fish struct {
		Abbreviations bool `toml:"abbreviations"`
		Greeting      bool `toml:"greeting"`
	} `toml:"fish"`
	Pwsh struct {
		ClobberBuiltinAliases bool `toml:"clobber_builtin_aliases"`
		UTF8                  bool `toml:"utf8"`
	} `toml:"pwsh"`
}

type Pkg struct {
	Prefer string `toml:"prefer"`
}

// DefaultPlugins is what a fresh install enables.
var DefaultPlugins = []string{"core", "git", "jump", "python", "node", "docker", "kubectl", "pkg"}

// Defaults returns the configuration used when keys are missing.
func Defaults() *Config {
	c := &Config{
		Theme:       "fire",
		Icons:       "auto",
		Color:       "auto",
		Background:  "dark",
		Greeting:    true,
		Suggest:     true,
		Completions: true,
		Plugins:     append([]string(nil), DefaultPlugins...),
		Path:        []string{"~/.local/bin", "~/bin"},
		Prompt: Prompt{
			DurationThreshold: 2,
			GitTimeoutMS:      200,
			MaxDirDepth:       4,
			Transient:         true,
			SemanticMarks:     true,
		},
		Core:    Core{Defaults: true},
		History: History{Shared: true, CtrlR: true},
	}
	c.Shell.Fish.Abbreviations = true
	c.Shell.Pwsh.UTF8 = true
	return c
}

// Path returns the config file location.
func Path() string { return filepath.Join(platform.ConfigDir(), "config.toml") }

// Load reads the config file. A missing file is not an error; a broken one
// returns defaults plus the error so the prompt keeps working.
func Load() (*Config, error) {
	c := Defaults()
	c.File = Path()
	b, err := os.ReadFile(c.File)
	if errors.Is(err, fs.ErrNotExist) {
		c.applyEnv()
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if _, err := toml.Decode(string(b), c); err != nil {
		d := Defaults()
		d.File = c.File
		d.applyEnv()
		return d, fmt.Errorf("%s: %w", c.File, err)
	}
	c.Loaded = true
	c.applyEnv()
	return c, nil
}

func (c *Config) applyEnv() {
	if t := os.Getenv("OPAL_THEME"); t != "" {
		c.Theme = t
	}
	if v := os.Getenv("OPAL_ICONS"); v != "" {
		c.Icons = v
	}
	if v := os.Getenv("OPAL_COLOR"); v != "" {
		c.Color = v
	}
	switch strings.ToLower(os.Getenv("OPAL_GREETING")) {
	case "0", "false", "off", "no":
		c.Greeting = false
	case "1", "true", "on", "yes":
		c.Greeting = true
	}
}

// HasPlugin reports whether a plugin is enabled.
func (c *Config) HasPlugin(name string) bool {
	for _, p := range c.Plugins {
		if p == name {
			return true
		}
	}
	return false
}

// WriteDefault creates the config file from the commented template if it
// does not exist yet. It reports whether a file was written.
func WriteDefault() (string, bool, error) {
	p := Path()
	if _, err := os.Stat(p); err == nil {
		return p, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, false, err
	}
	return p, true, os.WriteFile(p, assets.DefaultConfig, 0o644)
}

// SetKey rewrites a top-level `key = ...` line in place (keeping comments and
// layout), inserting it before the first table if it is missing.
func SetKey(key, tomlValue string) error {
	p := Path()
	if _, _, err := WriteDefault(); err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	src := string(b)
	line := key + " = " + tomlValue
	// Only look at the top-level section (before the first [table]).
	top := src
	rest := ""
	if loc := regexp.MustCompile(`(?m)^\s*\[`).FindStringIndex(src); loc != nil {
		top, rest = src[:loc[0]], src[loc[0]:]
	}
	re := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(key) + `\s*=\s*(\[.*?\]|[^\n]*)`)
	if re.MatchString(top) {
		top = re.ReplaceAllLiteralString(top, line)
	} else {
		if !strings.HasSuffix(top, "\n") && top != "" {
			top += "\n"
		}
		top += line + "\n\n"
	}
	return os.WriteFile(p, []byte(top+rest), 0o644)
}

// QuoteList renders a TOML array of strings.
func QuoteList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

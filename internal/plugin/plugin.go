// Package plugin loads declarative plugins and resolves them for one
// shell + OS, producing the aliases, functions, env vars and native snippets
// that the shell generators turn into real code.
package plugin

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"opal/internal/assets"
	"opal/internal/config"
	"opal/internal/platform"
)

// Plugin mirrors a plugin TOML file.
type Plugin struct {
	Name        string                     `toml:"name"`
	Description string                     `toml:"description"`
	Requires    []string                   `toml:"requires"`
	Env         map[string]config.Variants `toml:"env"`
	Aliases     map[string]config.Variants `toml:"aliases"`
	Functions   map[string]config.Variants `toml:"functions"`
	Native      config.Variants            `toml:"native"`

	Source string `toml:"-"` // "builtin" or a file path
}

// Available loads every plugin: built-ins, then user plugins from
// ~/.config/opal/plugins (<name>.toml or <name>/plugin.toml with optional
// init.<zsh|bash|sh|fish|ps1> files), which override built-ins by name.
func Available() (map[string]*Plugin, []error) {
	out := map[string]*Plugin{}
	var errs []error
	entries, _ := fs.ReadDir(assets.Plugins, "plugins")
	for _, e := range entries {
		b, err := assets.Plugins.ReadFile("plugins/" + e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := parse(b, "builtin")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out[p.Name] = p
	}
	dir := filepath.Join(platform.ConfigDir(), "plugins")
	user, err := os.ReadDir(dir)
	if err != nil {
		return out, errs
	}
	for _, e := range user {
		var file string
		switch {
		case e.IsDir():
			file = filepath.Join(dir, e.Name(), "plugin.toml")
		case strings.HasSuffix(e.Name(), ".toml"):
			file = filepath.Join(dir, e.Name())
		default:
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			if !e.IsDir() {
				errs = append(errs, err)
			}
			continue
		}
		p, err := parse(b, file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(e.Name(), ".toml")
		}
		if e.IsDir() {
			loadNativeFiles(p, filepath.Join(dir, e.Name()))
		}
		out[p.Name] = p
	}
	return out, errs
}

// loadNativeFiles lets directory plugins ship real shell files.
func loadNativeFiles(p *Plugin, dir string) {
	keys := map[string]string{"init.zsh": "zsh", "init.bash": "bash", "init.sh": "sh", "init.fish": "fish", "init.ps1": "pwsh"}
	for file, key := range keys {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			continue
		}
		if p.Native.Values == nil {
			p.Native.Values = map[string]string{}
		}
		p.Native.Values[key] = string(b)
	}
}

func parse(b []byte, source string) (*Plugin, error) {
	p := &Plugin{}
	if _, err := toml.Decode(string(b), p); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	p.Source = source
	return p, nil
}

// Def is one generated definition.
type Def struct {
	Name   string
	Body   string
	Desc   string
	Plugin string
	Func   bool // function (vs alias)
}

// Status explains what happened to a plugin for this target.
type Status struct {
	Name        string
	Description string
	Active      bool
	Reason      string
	Aliases     int
	Functions   int
	Source      string
}

// Result is everything a shell generator needs.
type Result struct {
	Status []Status
	Defs   []Def // aliases and functions, in definition order
	Env    []Def
	Native []Def
}

// Resolve applies the enabled plugins, then the user's own config, for t.
// has reports whether a command exists on PATH.
func Resolve(cfg *config.Config, t config.Target, has func(string) bool) Result {
	all, _ := Available()
	var r Result
	index := map[string]int{}
	envIndex := map[string]int{}

	add := func(d Def) {
		if i, ok := index[d.Name]; ok {
			r.Defs[i] = d // later definitions win, keeping the first position
			return
		}
		index[d.Name] = len(r.Defs)
		r.Defs = append(r.Defs, d)
	}
	addEnv := func(d Def) {
		if i, ok := envIndex[d.Name]; ok {
			r.Env[i] = d
			return
		}
		envIndex[d.Name] = len(r.Env)
		r.Env = append(r.Env, d)
	}
	applies := func(v config.Variants) (string, bool) {
		for _, c := range v.Requires {
			if !has(c) {
				return "", false
			}
		}
		for _, c := range v.Unless {
			if has(c) {
				return "", false
			}
		}
		return v.Pick(t)
	}

	for _, name := range cfg.Plugins {
		p, ok := all[name]
		if !ok {
			r.Status = append(r.Status, Status{Name: name, Reason: "not found"})
			continue
		}
		st := Status{Name: name, Description: p.Description, Source: p.Source, Active: true}
		for _, c := range p.Requires {
			if !has(c) {
				st.Active, st.Reason = false, "needs "+c
				break
			}
		}
		if !st.Active {
			r.Status = append(r.Status, st)
			continue
		}
		for _, k := range sortedKeys(p.Env) {
			if v, ok := applies(p.Env[k]); ok {
				addEnv(Def{Name: k, Body: v, Plugin: name})
			}
		}
		for _, k := range sortedKeys(p.Aliases) {
			if v, ok := applies(p.Aliases[k]); ok {
				add(Def{Name: k, Body: v, Desc: p.Aliases[k].Desc, Plugin: name})
				st.Aliases++
			}
		}
		for _, k := range sortedKeys(p.Functions) {
			if v, ok := applies(p.Functions[k]); ok {
				add(Def{Name: k, Body: v, Desc: p.Functions[k].Desc, Plugin: name, Func: true})
				st.Functions++
			}
		}
		if v, ok := p.Native.Pick(t); ok {
			r.Native = append(r.Native, Def{Name: name, Body: v, Plugin: name})
		}
		r.Status = append(r.Status, st)
	}

	// The user's own config comes last so it always wins.
	for _, k := range sortedKeys(cfg.Env) {
		if v, ok := applies(cfg.Env[k]); ok {
			addEnv(Def{Name: k, Body: v, Plugin: "config"})
		}
	}
	for _, k := range sortedKeys(cfg.Aliases) {
		if v, ok := applies(cfg.Aliases[k]); ok {
			add(Def{Name: k, Body: v, Desc: cfg.Aliases[k].Desc, Plugin: "config"})
		}
	}
	for _, k := range sortedKeys(cfg.Functions) {
		if v, ok := applies(cfg.Functions[k]); ok {
			add(Def{Name: k, Body: v, Desc: cfg.Functions[k].Desc, Plugin: "config", Func: true})
		}
	}
	return r
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

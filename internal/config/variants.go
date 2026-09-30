package config

import (
	"fmt"
)

// Variants is one alias, function, env var or native snippet that can differ
// per shell and per OS. In TOML it is either a plain string (same everywhere)
// or a table:
//
//	ll = { unix = "ls -lh", pwsh = "Get-ChildItem", requires = ["ls"] }
//
// Keys are tried most-specific first (see Target.Keys). An empty string at
// the winning key means "not in this shell / OS".
type Variants struct {
	Values   map[string]string
	Requires []string // every command must be on PATH
	Unless   []string // skipped if any of these commands exist
	Desc     string
}

// UnmarshalTOML implements toml.Unmarshaler.
func (v *Variants) UnmarshalTOML(data any) error {
	v.Values = map[string]string{}
	switch t := data.(type) {
	case string:
		v.Values["default"] = t
	case map[string]any:
		for k, raw := range t {
			switch k {
			case "requires", "unless":
				list, err := stringList(raw)
				if err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
				if k == "requires" {
					v.Requires = list
				} else {
					v.Unless = list
				}
			case "desc", "description":
				s, _ := raw.(string)
				v.Desc = s
			default:
				s, ok := raw.(string)
				if !ok {
					return fmt.Errorf("key %q must be a string", k)
				}
				if k == "cmd" || k == "body" {
					k = "default"
				}
				v.Values[k] = s
			}
		}
	default:
		return fmt.Errorf("expected a string or a table, got %T", data)
	}
	return nil
}

func stringList(raw any) ([]string, error) {
	switch t := raw.(type) {
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			s, ok := x.(string)
			if !ok {
				return nil, fmt.Errorf("expected strings, got %T", x)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected a list of strings, got %T", raw)
}

// Target is the shell + OS pair code is being generated for.
type Target struct {
	Shell string // zsh | bash | fish | pwsh
	OS    string // macos | linux | windows | ...
	WSL   bool
}

func (t Target) osKeys() []string {
	if t.WSL {
		return []string{"wsl", t.OS}
	}
	return []string{t.OS}
}

// Keys lists variant keys from most to least specific, e.g. for Git Bash:
// bash.windows, bash, sh.windows, sh, windows, default.
func (t Target) Keys() []string {
	var keys []string
	for _, o := range t.osKeys() {
		keys = append(keys, t.Shell+"."+o)
	}
	keys = append(keys, t.Shell)
	if t.Shell == "bash" || t.Shell == "zsh" {
		for _, o := range t.osKeys() {
			keys = append(keys, "sh."+o)
		}
		keys = append(keys, "sh")
	}
	keys = append(keys, t.osKeys()...)
	if t.OS != "windows" {
		keys = append(keys, "unix")
	}
	return append(keys, "default")
}

// Pick returns the value for t. ok is false when nothing applies or the
// winning key is explicitly empty.
func (v Variants) Pick(t Target) (string, bool) {
	for _, k := range t.Keys() {
		if s, found := v.Values[k]; found {
			return s, s != ""
		}
	}
	return "", false
}

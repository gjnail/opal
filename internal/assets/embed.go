// Package assets embeds the built-in plugins, themes, shell snippets and the
// default config into the binary, so opal is a single file.
package assets

import "embed"

//go:embed plugins/*.toml
var Plugins embed.FS

//go:embed themes/*.toml
var Themes embed.FS

//go:embed shell/*
var Shell embed.FS

//go:embed config.default.toml
var DefaultConfig []byte

//go:embed completions.toml
var Completions []byte

// ShellFile returns an embedded shell snippet (e.g. "core.zsh").
func ShellFile(name string) string {
	b, err := Shell.ReadFile("shell/" + name)
	if err != nil {
		return ""
	}
	return string(b)
}

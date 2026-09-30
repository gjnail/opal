package prompt

// Symbols is a glyph set. Themes can override Gem and Char.
type Symbols struct {
	Gem, Char, FrameTop, FrameBottom                   string
	On, Branch, Ahead, Behind                          string
	Staged, Modified, Untracked, Conflict, Stash       string
	Error, Jobs, Took, Venv, Ellipsis, Container, Root string
	Sep, SepEnd                                        string // block separators
}

var symbolSets = map[string]Symbols{
	"unicode": {
		Gem: "◆", Char: "❯", FrameTop: "╭─", FrameBottom: "╰─",
		On: "on", Branch: "", Ahead: "⇡", Behind: "⇣",
		Staged: "+", Modified: "!", Untracked: "?", Conflict: "✖", Stash: "≡",
		Error: "✘", Jobs: "✦", Took: "took", Venv: "◇", Ellipsis: "…", Container: "⬡", Root: "#",
	},
	"nerd": {
		Gem: "", Char: "❯", FrameTop: "╭─", FrameBottom: "╰─",
		On: "", Branch: "", Ahead: "⇡", Behind: "⇣",
		Staged: "+", Modified: "!", Untracked: "?", Conflict: "", Stash: "",
		Error: "", Jobs: "", Took: "", Venv: "", Ellipsis: "…", Container: "", Root: "",
		Sep: "", SepEnd: "",
	},
	"ascii": {
		Gem: "*", Char: ">", FrameTop: ".-", FrameBottom: "'-",
		On: "on", Branch: "", Ahead: "^", Behind: "v",
		Staged: "+", Modified: "!", Untracked: "?", Conflict: "x", Stash: "$",
		Error: "x", Jobs: "&", Took: "took", Venv: "py:", Ellipsis: "...", Container: "[ctr]", Root: "#",
	},
}

// SymbolSet returns the glyphs for "nerd", "unicode" or "ascii".
func SymbolSet(name string) Symbols {
	if s, ok := symbolSets[name]; ok {
		return s
	}
	return symbolSets["unicode"]
}

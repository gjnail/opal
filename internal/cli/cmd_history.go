package cli

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"opal/internal/config"
	"opal/internal/history"
	"opal/internal/picker"
	"opal/internal/platform"
	"opal/internal/shells"
	"opal/internal/theme"
)

func cmdHistory(args []string) int {
	if len(args) == 0 {
		args = []string{"search"}
	}
	switch args[0] {
	case "search":
		return historySearch(args[1:])
	case "import":
		return historyImport(false)
	case "list", "ls":
		n := 20
		if len(args) > 1 {
			if v, err := strconv.Atoi(args[1]); err == nil {
				n = v
			}
		}
		es, err := history.Load()
		if err != nil {
			errorf("%v", err)
			return 1
		}
		for _, e := range es[max(0, len(es)-n):] {
			fmt.Println(e.Cmd)
		}
		return 0
	}
	errorf("usage: opal history [search | import | list [n]]")
	return 2
}

// historyImport pulls in every shell's existing history file.
func historyImport(quiet bool) int {
	srcs := history.Sources()
	if len(srcs) == 0 {
		if !quiet {
			fmt.Println("no shell history files found")
		}
		return 0
	}
	n, err := history.Import(srcs)
	if err != nil {
		errorf("import: %v", err)
		return 1
	}
	if !quiet {
		u := newUI()
		for _, s := range srcs {
			fmt.Printf("  %s %-5s %s\n", u.ok("✓"), s.Shell, u.muted(s.File))
		}
		fmt.Printf("  imported %d commands into %s\n", n, u.muted(history.File()))
	}
	return 0
}

// historySearch runs the Ctrl+R picker and prints the chosen command.
func historySearch(args []string) int {
	shell, query, cwd := currentShell(), "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--shell":
			shell = next()
		case a == "--query":
			query = next()
		case a == "--query64":
			if b, err := base64.StdEncoding.DecodeString(next()); err == nil {
				query = string(b)
			}
		case a == "--cwd":
			cwd = next()
		case strings.HasPrefix(a, "--query="):
			query = strings.TrimPrefix(a, "--query=")
		}
	}
	shell = shells.Normalize(shell)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if !history.Exists() {
		historyImport(true) // first use: start from what the shells already remember
	}
	entries, err := history.Load()
	if err != nil {
		errorf("%v", err)
		return 1
	}
	cmds := history.Unique(entries)

	cfg, _ := config.Load()
	th, _ := theme.LoadOrDefault(cfg.Theme)
	pal := th.Resolve(cfg.Background, shell)
	depth := platform.ColorDepth(cfg.Color)
	sym := platformSymbols(cfg)
	style := picker.Style{
		Accent: pal.Hue.FG(depth) + ";1",
		Muted:  pal.C("muted").FG(depth),
		Text:   pal.C("text").FG(depth),
		Match:  pal.C("branch").FG(depth) + ";1",
		SelBG:  pal.C("frame").BG(depth),
		Gem:    sym[0],
		Char:   sym[1],
	}
	if depth == 0 {
		style = picker.Style{Accent: "1", Match: "1;4", SelBG: "7", Gem: sym[0], Char: sym[1]}
	}
	now := time.Now().Unix()
	home := platform.Home()
	scopes := []string{"all shells", "this directory", "this shell"}
	filter := func(q string, scope int) ([]picker.Item, int) {
		found := history.Search(cmds, q, history.Scope(scope), cwd, shell)
		items := make([]picker.Item, 0, min(len(found), 5000))
		for i, c := range found {
			if i == 5000 {
				break
			}
			items = append(items, picker.Item{Text: c.Cmd, Meta: meta(c, now, home)})
		}
		return items, len(cmds)
	}
	choice, ok, err := picker.Run(picker.Options{
		Title: "history", Query: query, Scopes: scopes, Filter: filter, Style: style,
	})
	if err != nil {
		errorf("history: %v", err)
		return 1
	}
	if !ok {
		return 1
	}
	fmt.Print(choice)
	return 0
}

func platformSymbols(cfg *config.Config) [2]string {
	switch platform.IconSet(cfg.Icons) {
	case "ascii":
		return [2]string{"*", ">"}
	}
	return [2]string{"◆", "❯"}
}

// meta is the dim right-hand column: when, which shell, where.
func meta(c history.Command, now int64, home string) string {
	parts := []string{ago(now - c.When)}
	if sh := strings.TrimPrefix(c.Shell, "import:"); sh != "" {
		parts = append(parts, sh)
	}
	if c.Count > 1 {
		parts = append(parts, "×"+strconv.Itoa(c.Count))
	}
	if c.Cwd != "" {
		d := c.Cwd
		if home != "" && len(d) >= len(home) && strings.EqualFold(d[:len(home)], home) {
			d = "~" + d[len(home):]
		}
		if len([]rune(d)) > 28 {
			r := []rune(d)
			d = "…" + string(r[len(r)-27:])
		}
		parts = append(parts, d)
	}
	return strings.Join(parts, " · ")
}

func ago(s int64) string {
	switch {
	case s < 0 || s > 100*365*86400:
		return "long ago"
	case s < 60:
		return "now"
	case s < 3600:
		return strconv.FormatInt(s/60, 10) + "m ago"
	case s < 86400:
		return strconv.FormatInt(s/3600, 10) + "h ago"
	case s < 30*86400:
		return strconv.FormatInt(s/86400, 10) + "d ago"
	case s < 365*86400:
		return strconv.FormatInt(s/(30*86400), 10) + "mo ago"
	}
	return strconv.FormatInt(s/(365*86400), 10) + "y ago"
}

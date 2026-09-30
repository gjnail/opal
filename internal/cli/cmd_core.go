package cli

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"opal/internal/config"
	"opal/internal/greet"
	"opal/internal/history"
	"opal/internal/jump"
	"opal/internal/platform"
	"opal/internal/prompt"
	"opal/internal/shells"
	"opal/internal/theme"
)

func cmdInit(args []string) int {
	if len(args) == 0 {
		errorf("usage: opal init <%s>", "zsh|bash|fish|pwsh")
		return 2
	}
	shell := shells.Normalize(args[0])
	cfg, cfgErr := config.Load()
	th, thErr := theme.LoadOrDefault(cfg.Theme)
	if thErr != nil && cfgErr == nil {
		cfgErr = thErr
	}
	script, err := shells.Generate(shell, shells.Options{
		Cfg:     cfg,
		CfgErr:  cfgErr,
		Theme:   th,
		Bin:     binPath(),
		Info:    platform.Detect(),
		Has:     platform.HasCommand,
		TermID:  platform.TerminalID(),
		Greeted: os.Getenv("OPAL_GREETED"),
	})
	if err != nil {
		errorf("%v", err)
		return 2
	}
	fmt.Print(shells.Wrap(shell, script))
	return 0
}

// cmdPrompt is the hot path: it runs on every prompt, so it does as little
// as possible and never fails loudly.
func cmdPrompt(args []string) int {
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	shell := fs.String("shell", currentShell(), "")
	status := fs.Int("status", 0, "")
	duration := fs.Int64("duration", -1, "")
	jobs := fs.Int("jobs", 0, "")
	width := fs.Int("width", 0, "")
	cwd := fs.String("cwd", "", "")
	record := fs.Bool("record", false, "")
	// The command that just finished, for the shared history. PowerShell
	// sends it base64-encoded: Windows PowerShell mangles embedded quotes
	// when passing arguments to native programs.
	cmdText := fs.String("cmd", "", "")
	cmd64 := fs.String("cmd64", "", "")
	cmdID := fs.String("cmd-id", "", "")
	_ = fs.Parse(args)
	if *cmd64 != "" {
		if b, err := base64.StdEncoding.DecodeString(*cmd64); err == nil {
			*cmdText = string(b)
		}
	}

	cfg, cfgErr := config.Load()
	th, thErr := theme.LoadOrDefault(cfg.Theme)

	c := prompt.NewContext(cfg, th, shells.Normalize(*shell))
	c.Status, c.DurationMS, c.Jobs, c.Width = *status, *duration, *jobs, *width
	c.Cwd = prompt.Cwd(*cwd)
	c.GitCacheDir = gitCacheDir()
	c.RefreshGit = refreshGitInBackground

	var wg sync.WaitGroup
	if *record && cfg.HasPlugin("jump") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if fi, err := os.Stat(c.Cwd); err == nil && fi.IsDir() {
				_ = jump.Add(c.Cwd)
			}
		}()
	}

	if *cmdText != "" && cfg.History.Shared {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cwd := c.Cwd
			if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
				cwd = ""
			}
			_ = history.Append(history.Entry{Status: c.Status, DurMS: c.DurationMS, Shell: c.Shell,
				ID: *cmdID, Cwd: cwd, Cmd: *cmdText})
		}()
	}

	depth := platform.ColorDepth(cfg.Color)
	lines := prompt.Render(c)
	if cfgErr != nil || thErr != nil {
		warn := prompt.Span{Text: "⚠ config ", FG: c.Pal.C("error"), Bold: true}
		lines[len(lines)-1] = append([]prompt.Span{warn}, lines[len(lines)-1]...)
	}
	if prompt.MarksEnabled(cfg.Prompt.SemanticMarks, depth) {
		lines = prompt.WithMarks(lines, c)
	}
	fmt.Print(prompt.Encode(lines, c.Shell, depth))
	wg.Wait()
	return 0
}

// cmdGreet prints the banner. The init script calls it with --hook once per
// new terminal; PowerShell captures our output there, so color regardless.
func cmdGreet(args []string) int {
	fs := flag.NewFlagSet("greet", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	hook := fs.Bool("hook", false, "")
	width := fs.Int("width", 0, "")
	_ = fs.Parse(args)

	cfg, _ := config.Load()
	th, _ := theme.LoadOrDefault(cfg.Theme)
	depth := platform.ColorDepth(cfg.Color)
	if !*hook && !isTerminal() {
		depth = 0
	}
	w := *width
	if w <= 0 {
		w = platform.TermWidth()
	}
	lines := greet.Render(th.Resolve(cfg.Background, currentShell()), platform.IconSet(cfg.Icons), w)
	fmt.Println(prompt.Encode(lines, "", depth))
	return 0
}

func cmdJump(args []string) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "query":
		words := args[1:]
		if len(words) > 0 && words[0] == "--" {
			words = words[1:]
		}
		shell := currentShell()
		// `j some/dir` or `j ..` behaves like cd when the argument exists.
		if len(words) == 1 {
			p := platform.FromMSYS(words[0])
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				fmt.Println(platform.ShellPath(absPath(p), shell))
				return 0
			}
		}
		cwd, _ := os.Getwd()
		if p, ok := jump.Query(words, cwd); ok {
			fmt.Println(platform.ShellPath(p, shell))
			return 0
		}
		errorf("no match for %q (visit it once with cd, then j finds it)", words)
		return 1
	case "list":
		paths := len(args) > 1 && args[1] == "--paths"
		entries, err := jump.Ranked()
		if err != nil {
			errorf("%v", err)
			return 1
		}
		shell := currentShell()
		for _, e := range entries {
			if paths {
				fmt.Println(platform.ShellPath(e.Path, shell))
			} else {
				fmt.Printf("%8.1f  %s\n", jump.Score(e, nowUnix()), platform.ShellPath(e.Path, shell))
			}
		}
		return 0
	case "add", "remove":
		if len(args) < 2 {
			errorf("usage: opal jump %s <dir>", args[0])
			return 2
		}
		p := absPath(platform.FromMSYS(args[1]))
		var err error
		if args[0] == "add" {
			err = jump.Add(p)
		} else {
			err = jump.Remove(p)
		}
		if err != nil {
			errorf("%v", err)
			return 1
		}
		return 0
	}
	errorf("usage: opal jump query <words> | list | add <dir> | remove <dir>")
	return 2
}

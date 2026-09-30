package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndRepeats(t *testing.T) {
	t.Setenv("OPAL_DATA_DIR", t.TempDir())
	cmds := []Entry{
		{Shell: "pwsh", Cwd: `C:\src`, Cmd: "git status"},
		{Shell: "bash", ID: "42:7", Cmd: "echo 'a\tb'\nsecond line"},
		{Shell: "bash", ID: "42:7", Cmd: "echo 'a\tb'\nsecond line"}, // same history number: a repeat
		{Shell: "zsh", Cmd: " secret --token x"},                     // leading space: private
	}
	for _, e := range cmds {
		if err := Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if got[1].Cmd != "echo 'a\tb'\nsecond line" || got[0].Cwd != `C:\src` {
		t.Errorf("escaping lost data: %+v", got)
	}
}

func TestSearchRanking(t *testing.T) {
	es := []Entry{
		{When: 1, Shell: "zsh", Cwd: "/a", Cmd: "docker compose up"},
		{When: 2, Shell: "pwsh", Cwd: "/b", Cmd: "git commit -m wip"},
		{When: 3, Shell: "bash", Cwd: "/a", Cmd: "echo git is great"},
		{When: 4, Shell: "zsh", Cwd: "/a", Cmd: "git status"},
		{When: 5, Shell: "zsh", Cwd: "/a", Cmd: "git status"},
	}
	cmds := Unique(es)
	if len(cmds) != 4 || cmds[0].Cmd != "git status" || cmds[0].Count != 2 {
		t.Fatalf("unique = %+v", cmds)
	}
	got := Search(cmds, "git", AllShells, "", "")
	want := []string{"git status", "git commit -m wip", "echo git is great"}
	if len(got) != 3 {
		t.Fatalf("search git = %+v", got)
	}
	for i, w := range want {
		if got[i].Cmd != w {
			t.Errorf("rank %d = %q, want %q (prefix matches first, then recency)", i, got[i].Cmd, w)
		}
	}
	if got := Search(cmds, "git", ThisDir, "/b", ""); len(got) != 1 || got[0].Cmd != "git commit -m wip" {
		t.Errorf("dir scope = %+v", got)
	}
	if got := Search(cmds, "dcu", AllShells, "", ""); len(got) != 1 || got[0].Cmd != "docker compose up" {
		t.Errorf("fuzzy fallback = %+v", got)
	}
	if got := Search(cmds, "^git status$", AllShells, "", ""); len(got) != 1 {
		t.Errorf("anchors = %+v", got)
	}
}

func TestImportFormats(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	if es := parsePSReadLine(write("ps.txt", "git status\r\nfunction f {`\r\n  1`\r\n}\r\n")); len(es) != 2 || es[1].Cmd != "function f {\n  1\n}" {
		t.Errorf("psreadline: %+v", es)
	}
	if es := parseBash(write("bash", "#1700000000\nls -la\nwhoami\n")); len(es) < 2 || es[0].When != 1700000000 || es[1].When != 0 {
		t.Errorf("bash: %+v", es)
	}
	if es := parseZsh(write("zsh", ": 1700000001:3;make test\n: 1700000002:0;echo a\\\nb\n")); len(es) < 2 || es[0].Cmd != "make test" || es[0].DurMS != 3000 || es[1].Cmd != "echo a\nb" {
		t.Errorf("zsh: %+v", es)
	}
	if es := parseFish(write("fish", "- cmd: ls\n  when: 1700000003\n- cmd: echo a\\nb\n  when: 1700000004\n")); len(es) != 2 || es[1].Cmd != "echo a\nb" || es[1].When != 1700000004 {
		t.Errorf("fish: %+v", es)
	}
}

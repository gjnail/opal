package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupBlockIsIdempotent(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	os.WriteFile(rc, []byte("export FOO=1\n"), 0o644)
	tgt := rcTarget{shell: "zsh", file: rc, body: `eval "$(opal init zsh)"`}

	if v, err := applyTarget(tgt, false, false); err != nil || v != "added" {
		t.Fatalf("first apply = %q %v", v, err)
	}
	if v, _ := applyTarget(tgt, false, false); v != "unchanged" {
		t.Errorf("second apply = %q, want unchanged", v)
	}
	b, _ := os.ReadFile(rc)
	if strings.Count(string(b), markStart) != 1 || !strings.HasPrefix(string(b), "export FOO=1\n") {
		t.Errorf("rc after setup:\n%s", b)
	}

	tgt.body = `eval "$(opal init zsh --x)"`
	if v, _ := applyTarget(tgt, false, false); v != "updated" {
		t.Errorf("changed body = %q, want updated", v)
	}
	if v, _ := applyTarget(tgt, true, false); v != "removed" {
		t.Errorf("remove = %q", v)
	}
	b, _ = os.ReadFile(rc)
	if string(b) != "export FOO=1\n" {
		t.Errorf("remove should restore the original file, got %q", b)
	}
}

func TestSetupKeepsCRLF(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "profile.ps1")
	os.WriteFile(rc, []byte("Set-Alias ll ls\r\n"), 0o644)
	applyTarget(rcTarget{file: rc, body: "Invoke-Expression x"}, false, false)
	b, _ := os.ReadFile(rc)
	if strings.Contains(strings.ReplaceAll(string(b), "\r\n", ""), "\n") {
		t.Errorf("mixed line endings: %q", b)
	}
}

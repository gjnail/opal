package pty

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func shellCmd(t *testing.T, script string) Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		p, err := exec.LookPath("cmd.exe")
		if err != nil {
			t.Skip("no cmd.exe")
		}
		return Cmd{Path: p, Args: []string{"cmd.exe", "/c", script}, Env: os.Environ(), Cols: 80, Rows: 24}
	}
	return Cmd{Path: "/bin/sh", Args: []string{"sh", "-c", script}, Env: os.Environ(), Cols: 80, Rows: 24}
}

// readAll drains the PTY until EOF or timeout.
func readAll(t *testing.T, p PTY, d time.Duration) string {
	t.Helper()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, p)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		p.Close()
		<-done
	}
	return buf.String()
}

func TestRunEcho(t *testing.T) {
	p, err := Start(shellCmd(t, "echo opal-pty-ok"))
	if err != nil {
		t.Fatal(err)
	}
	var code int
	waited := make(chan struct{})
	go func() {
		code, _ = p.Wait()
		close(waited)
	}()
	out := readAll(t, p, 10*time.Second)
	<-waited
	if !strings.Contains(out, "opal-pty-ok") {
		t.Fatalf("output %q", out)
	}
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	p.Close()
}

func TestExitCode(t *testing.T) {
	p, err := Start(shellCmd(t, "exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, p)
	code, _ := p.Wait()
	p.Close()
	if code != 3 {
		t.Fatalf("exit code %d, want 3", code)
	}
}

func TestSizeReachesChild(t *testing.T) {
	c := shellCmd(t, "stty size")
	if runtime.GOOS == "windows" {
		ps, err := exec.LookPath("powershell.exe")
		if err != nil {
			t.Skip("no powershell")
		}
		c = Cmd{Path: ps, Args: []string{"powershell", "-NoProfile", "-Command", "[Console]::WindowWidth"}, Env: os.Environ()}
	}
	c.Cols, c.Rows = 97, 31
	p, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	out := readAll(t, p, 30*time.Second)
	p.Close()
	if !strings.Contains(out, "97") {
		t.Fatalf("child didn't see 97 columns: %q", out)
	}
}

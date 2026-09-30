//go:build !windows

package pty

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	cpty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type unixPTY struct {
	f    *os.File
	cmd  *exec.Cmd
	once sync.Once
	code int
	err  error
	done chan struct{}
}

// Start launches c on a new pseudo terminal.
func Start(c Cmd) (PTY, error) {
	if len(c.Args) == 0 {
		c.Args = []string{c.Path}
	}
	cmd := exec.Command(c.Path, c.Args[1:]...)
	cmd.Args[0] = c.Args[0]
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	f, err := cpty.StartWithSize(cmd, &cpty.Winsize{Rows: uint16(c.Rows), Cols: uint16(c.Cols)})
	if err != nil {
		return nil, err
	}
	p := &unixPTY{f: f, cmd: cmd, done: make(chan struct{})}
	go p.reap()
	return p, nil
}

func (p *unixPTY) reap() {
	err := p.cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		p.code = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			p.code = 128 + int(ws.Signal())
		}
	default:
		p.err = err
		p.code = -1
	}
	close(p.done)
}

func (p *unixPTY) Read(b []byte) (int, error) {
	n, err := p.f.Read(b)
	if err != nil && errors.Is(err, syscall.EIO) {
		// Linux reports EIO on the master once the child side closes.
		err = errEOF
	}
	return n, err
}

func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *unixPTY) Resize(cols, rows, pxW, pxH int) error {
	return cpty.Setsize(p.f, &cpty.Winsize{Rows: uint16(rows), Cols: uint16(cols), X: uint16(pxW), Y: uint16(pxH)})
}

func (p *unixPTY) Wait() (int, error) {
	<-p.done
	return p.code, p.err
}

func (p *unixPTY) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	// Hang up like a closing terminal would, then insist.
	_ = p.cmd.Process.Signal(syscall.SIGHUP)
	select {
	case <-p.done:
		return nil
	default:
	}
	return p.cmd.Process.Kill()
}

func (p *unixPTY) Close() error {
	var err error
	p.once.Do(func() { err = p.f.Close() })
	return err
}

func (p *unixPTY) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *unixPTY) Foreground() (string, int) {
	pgid, err := unix.IoctlGetInt(int(p.f.Fd()), unix.TIOCGPGRP)
	if err != nil || pgid <= 0 {
		return "", 0
	}
	return processName(pgid), pgid
}

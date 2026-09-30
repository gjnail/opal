// Package pty runs a child process attached to a pseudo terminal: a real
// PTY on Unix, ConPTY on Windows.
package pty

import "io"

// Cmd describes the process to start.
type Cmd struct {
	// Path is the executable. Args is the full argv, including argv[0]
	// (which may differ from Path, e.g. "-zsh" for a login shell).
	Path string
	Args []string
	Dir  string
	Env  []string
	Cols int
	Rows int
	// Graphemes tells a ConPTY that supports it to measure text by grapheme
	// cluster, matching the terminal (mode 2027).
	Graphemes bool
}

// PTY is a running child process and its terminal.
type PTY interface {
	io.Reader // output from the child
	io.Writer // input to the child
	// Resize tells the child the new grid (and pixel) size.
	Resize(cols, rows, pxW, pxH int) error
	// Wait blocks until the child exits and returns its exit code.
	Wait() (int, error)
	// Kill ends the child.
	Kill() error
	// Close releases the terminal. Reads return io.EOF afterwards.
	Close() error
	Pid() int
	// Foreground returns the name and pid of the process currently in the
	// foreground (the shell, or whatever it's running), when the platform
	// can tell.
	Foreground() (name string, pid int)
}

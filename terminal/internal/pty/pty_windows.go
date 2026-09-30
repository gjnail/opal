//go:build windows

package pty

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procUpdateProcThreadAttribute = kernel32.NewProc("UpdateProcThreadAttribute")
)

type conPTY struct {
	hpc     windows.Handle
	in      *os.File
	out     *os.File
	proc    windows.Handle
	pid     int
	done    chan struct{}
	code    int
	err     error
	closeMu sync.Mutex
	closed  bool
}

// Start launches c attached to a new pseudo console.
func Start(c Cmd) (PTY, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, err
	}
	var hpc windows.Handle
	size := windows.Coord{X: int16(max(c.Cols, 1)), Y: int16(max(c.Rows, 1))}
	if err := windows.CreatePseudoConsole(size, inR, outW, 0, &hpc); err != nil {
		for _, h := range []windows.Handle{inR, inW, outR, outW} {
			windows.CloseHandle(h)
		}
		return nil, err
	}
	// The pseudo console holds its own copies of the child's ends.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)

	p := &conPTY{
		hpc:  hpc,
		in:   os.NewFile(uintptr(inW), "conpty-in"),
		out:  os.NewFile(uintptr(outR), "conpty-out"),
		done: make(chan struct{}),
	}
	if err := p.spawn(c); err != nil {
		windows.ClosePseudoConsole(hpc)
		p.in.Close()
		p.out.Close()
		return nil, err
	}
	go p.reap()
	return p, nil
}

func (p *conPTY) spawn(c Cmd) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON itself, not a pointer to it.
	r, _, e := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(p.hpc), unsafe.Sizeof(p.hpc), 0, 0)
	if r == 0 {
		return e
	}

	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Don't leak our own std handles (a GUI app may have none, or a
	// console we were started from) into the child.
	si.Flags = windows.STARTF_USESTDHANDLES

	args := c.Args
	if len(args) == 0 {
		args = []string{c.Path}
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return err
	}
	var app *uint16
	if c.Path != "" {
		if app, err = windows.UTF16PtrFromString(c.Path); err != nil {
			return err
		}
	}
	var dir *uint16
	if c.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(c.Dir); err != nil {
			return err
		}
	}
	env := envBlock(c.Env)
	pi := new(windows.ProcessInformation)
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(app, cmdline, nil, nil, false, flags, &env[0], dir, &si.StartupInfo, pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Thread)
	p.proc = pi.Process
	p.pid = int(pi.ProcessId)
	return nil
}

// envBlock builds a sorted, de-duplicated UTF-16 environment block.
func envBlock(env []string) []uint16 {
	if len(env) == 0 {
		env = os.Environ()
	}
	byKey := map[string]string{}
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			// Windows keeps per-drive cwd entries like "=C:=C:\x".
			if strings.HasPrefix(kv, "=") {
				byKey[kv] = kv
			}
			continue
		}
		byKey[strings.ToUpper(k)] = kv
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []uint16
	for _, k := range keys {
		b = append(b, utf16.Encode([]rune(byKey[k]))...)
		b = append(b, 0)
	}
	return append(b, 0)
}

func (p *conPTY) reap() {
	windows.WaitForSingleObject(p.proc, windows.INFINITE)
	var code uint32
	if err := windows.GetExitCodeProcess(p.proc, &code); err != nil {
		p.err = err
		p.code = -1
	} else {
		p.code = int(int32(code))
	}
	close(p.done)
	// The console host keeps the output pipe open until the pseudo
	// console goes away; closing it now lets pending reads end with EOF.
	go p.closeConsole()
}

func (p *conPTY) closeConsole() {
	p.closeMu.Lock()
	hpc := p.hpc
	p.hpc = 0
	p.closeMu.Unlock()
	if hpc != 0 {
		windows.ClosePseudoConsole(hpc)
	}
}

func (p *conPTY) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.ERROR_BROKEN_PIPE)) {
		err = errEOF
	}
	return n, err
}

func (p *conPTY) Write(b []byte) (int, error) { return p.in.Write(b) }

func (p *conPTY) Resize(cols, rows, pxW, pxH int) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.hpc == 0 {
		return nil
	}
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(max(cols, 1)), Y: int16(max(rows, 1))})
}

func (p *conPTY) Wait() (int, error) {
	<-p.done
	return p.code, p.err
}

func (p *conPTY) Kill() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	// Closing the pseudo console sends CTRL_CLOSE_EVENT to everything
	// attached, which is what closing a console window does.
	go p.closeConsole()
	return nil
}

func (p *conPTY) Close() error {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return nil
	}
	p.closed = true
	p.closeMu.Unlock()
	// ClosePseudoConsole can block until output is drained, so it runs in
	// the background while the reader (if any) keeps draining.
	go func() {
		p.closeConsole()
		p.in.Close()
		p.out.Close()
		select {
		case <-p.done:
			windows.CloseHandle(p.proc)
		default:
		}
	}()
	return nil
}

func (p *conPTY) Pid() int { return p.pid }

// Foreground picks the most recently started descendant of the shell,
// which is the closest Windows gets to a foreground process group.
func (p *conPTY) Foreground() (string, int) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return "", 0
	}
	defer windows.CloseHandle(snap)
	type proc struct {
		pid, parent uint32
		name        string
	}
	var procs []proc
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs = append(procs, proc{e.ProcessID, e.ParentProcessID, windows.UTF16ToString(e.ExeFile[:])})
	}
	children := map[uint32][]proc{}
	var root proc
	for _, pr := range procs {
		children[pr.parent] = append(children[pr.parent], pr)
		if int(pr.pid) == p.pid {
			root = pr
		}
	}
	if root.pid == 0 {
		return "", 0
	}
	best, bestTime := root, int64(0)
	var walk func(pr proc, depth int)
	walk = func(pr proc, depth int) {
		if depth > 16 {
			return
		}
		for _, ch := range children[pr.pid] {
			n := strings.ToLower(ch.name)
			if n == "conhost.exe" || n == "openconsole.exe" {
				continue
			}
			if t := startTime(ch.pid); t >= bestTime {
				best, bestTime = ch, t
			}
			walk(ch, depth+1)
		}
	}
	walk(root, 0)
	return strings.TrimSuffix(filepath.Base(best.name), filepath.Ext(best.name)), int(best.pid)
}

func startTime(pid uint32) int64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var c, e, k, u windows.Filetime
	if windows.GetProcessTimes(h, &c, &e, &k, &u) != nil {
		return 0
	}
	return c.Nanoseconds()
}

// ProcessCWD isn't readable from outside a process on Windows without
// reading its PEB; shell integration (OSC 7 / OSC 9;9) covers it.
func ProcessCWD(pid int) string { return "" }

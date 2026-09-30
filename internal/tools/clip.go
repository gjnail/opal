package tools

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"

	"opal/internal/platform"
)

// ClipBackend names the mechanism Copy/Paste will use here.
func ClipBackend() string {
	in := platform.Detect()
	switch {
	case runtime.GOOS == "windows":
		return "win32"
	case runtime.GOOS == "darwin":
		return "pbcopy"
	case in.SSH && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "":
		return "osc52" // the clipboard you want is on the machine you're sitting at
	case in.WSL:
		return "clip.exe"
	case in.Termux && platform.HasCommand("termux-clipboard-set"):
		return "termux"
	case os.Getenv("WAYLAND_DISPLAY") != "" && platform.HasCommand("wl-copy"):
		return "wl-clipboard"
	case platform.HasCommand("xclip"):
		return "xclip"
	case platform.HasCommand("xsel"):
		return "xsel"
	}
	return "osc52"
}

// Copy puts text on the clipboard.
func Copy(text string) error {
	switch b := ClipBackend(); b {
	case "win32":
		return winCopy(text)
	case "pbcopy":
		return pipeTo(text, "pbcopy")
	case "clip.exe":
		// clip.exe reads UTF-16LE when it sees a BOM; otherwise it mangles non-ASCII.
		u := utf16.Encode([]rune(text))
		buf := []byte{0xff, 0xfe}
		for _, c := range u {
			buf = append(buf, byte(c), byte(c>>8))
		}
		cmd := exec.Command("clip.exe")
		cmd.Stdin = bytes.NewReader(buf)
		return cmd.Run()
	case "termux":
		return pipeTo(text, "termux-clipboard-set")
	case "wl-clipboard":
		return pipeTo(text, "wl-copy")
	case "xclip":
		return pipeTo(text, "xclip", "-selection", "clipboard")
	case "xsel":
		return pipeTo(text, "xsel", "--clipboard", "--input")
	default:
		return osc52(text)
	}
}

// Paste returns the clipboard contents.
func Paste() (string, error) {
	switch b := ClipBackend(); b {
	case "win32":
		return winPaste()
	case "pbcopy":
		return output("pbpaste")
	case "clip.exe":
		s, err := output("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-Clipboard -Raw")
		return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), err
	case "termux":
		return output("termux-clipboard-get")
	case "wl-clipboard":
		return output("wl-paste", "--no-newline")
	case "xclip":
		return output("xclip", "-selection", "clipboard", "-o")
	case "xsel":
		return output("xsel", "--clipboard", "--output")
	default:
		return "", errors.New("paste isn't possible over OSC 52; no local clipboard tool found")
	}
}

func pipeTo(text string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// osc52 asks the terminal itself to set the clipboard. It works over SSH and
// inside tmux (wrapped in a passthrough) in most modern terminals.
func osc52(text string) error {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	if os.Getenv("TMUX") != "" {
		seq = "\x1bPtmux;\x1b" + seq + "\x1b\\"
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		_, err = fmt.Fprint(os.Stderr, seq)
		return err
	}
	defer tty.Close()
	_, err = tty.WriteString(seq)
	return err
}

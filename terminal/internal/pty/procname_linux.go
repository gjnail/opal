package pty

import (
	"os"
	"strconv"
	"strings"
)

func processName(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ProcessCWD returns a process's working directory.
func ProcessCWD(pid int) string {
	d, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return d
}

package ui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	debugOnce sync.Once
	debugFile *os.File
	debugMu   sync.Mutex
)

// debugf appends to the file named by OPAL_TERMINAL_DEBUG, if set.
func debugf(format string, args ...any) {
	debugOnce.Do(func() {
		if path := os.Getenv("OPAL_TERMINAL_DEBUG"); path != "" {
			debugFile, _ = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		}
	})
	if debugFile == nil {
		return
	}
	debugMu.Lock()
	defer debugMu.Unlock()
	fmt.Fprintf(debugFile, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

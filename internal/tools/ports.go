package tools

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"opal/internal/platform"
)

// Listener is one listening TCP socket.
type Listener struct {
	Port    int
	Addrs   []string
	PID     int
	Process string
}

// Ports lists listening TCP ports using whatever this OS offers: ss/netstat
// on Linux, lsof on macOS, netstat + tasklist on Windows.
func Ports() ([]Listener, error) {
	var raw []Listener
	var err error
	switch runtime.GOOS {
	case "windows":
		raw, err = portsWindows()
	case "darwin":
		raw, err = portsLsof()
	default:
		switch {
		case platform.HasCommand("ss"):
			raw, err = portsSS()
		case platform.HasCommand("lsof"):
			raw, err = portsLsof()
		default:
			err = errors.New("need ss (iproute2) or lsof")
		}
	}
	if err != nil {
		return nil, err
	}
	return mergeListeners(raw), nil
}

func mergeListeners(in []Listener) []Listener {
	type key struct{ port, pid int }
	idx := map[key]int{}
	var out []Listener
	for _, l := range in {
		k := key{l.Port, l.PID}
		if i, ok := idx[k]; ok {
			for _, a := range l.Addrs {
				if !contains(out[i].Addrs, a) {
					out[i].Addrs = append(out[i].Addrs, a)
				}
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].PID < out[j].PID
	})
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// splitHostPort handles 0.0.0.0:80, [::]:80, *:80 and 127.0.0.53%lo:53.
func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndexAny(s, ":.")
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return "", 0, false
	}
	host := strings.Trim(s[:i], "[]")
	if j := strings.IndexByte(host, '%'); j >= 0 {
		host = host[:j]
	}
	return host, port, true
}

var ssProc = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

func portsSS() ([]Listener, error) {
	out, err := exec.Command("ss", "-H", "-ltnp").Output()
	if err != nil {
		return nil, err
	}
	var ls []Listener
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		host, port, ok := splitHostPort(f[3])
		if !ok {
			continue
		}
		l := Listener{Port: port, Addrs: []string{host}}
		if m := ssProc.FindStringSubmatch(sc.Text()); m != nil {
			l.Process = m[1]
			l.PID, _ = strconv.Atoi(m[2])
		}
		ls = append(ls, l)
	}
	return ls, nil
}

func portsLsof() ([]Listener, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn").Output()
	if err != nil && len(out) == 0 {
		return nil, nil // lsof exits 1 when nothing matches
	}
	var ls []Listener
	var pid int
	var name string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'c':
			name = line[1:]
		case 'n':
			if host, port, ok := splitHostPort(line[1:]); ok {
				ls = append(ls, Listener{Port: port, Addrs: []string{host}, PID: pid, Process: name})
			}
		}
	}
	return ls, nil
}

func portsWindows() ([]Listener, error) {
	out, err := exec.Command("netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return nil, err
	}
	out6, _ := exec.Command("netstat", "-ano", "-p", "TCPv6").Output()
	names := map[int]string{}
	if tl, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output(); err == nil {
		r := csv.NewReader(bytes.NewReader(tl))
		r.FieldsPerRecord = -1
		if rows, err := r.ReadAll(); err == nil {
			for _, row := range rows {
				if len(row) >= 2 {
					if pid, err := strconv.Atoi(row[1]); err == nil {
						names[pid] = strings.TrimSuffix(row[0], ".exe")
					}
				}
			}
		}
	}
	var ls []Listener
	for _, line := range strings.Split(string(out)+"\n"+string(out6), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "TCP") || f[3] != "LISTENING" {
			continue
		}
		host, port, ok := splitHostPort(f[1])
		if !ok {
			continue
		}
		pid, _ := strconv.Atoi(f[4])
		ls = append(ls, Listener{Port: port, Addrs: []string{host}, PID: pid, Process: names[pid]})
	}
	return ls, nil
}

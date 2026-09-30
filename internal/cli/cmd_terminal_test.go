package cli

import "testing"

func TestTerminalFlags(t *testing.T) {
	cases := []struct {
		args        []string
		dir, prints bool
	}{
		{nil, false, false},
		{[]string{"-dir", "/tmp"}, true, false},
		{[]string{"--dir=/tmp"}, true, false},
		{[]string{"-version"}, false, true},
		{[]string{"--list-profiles"}, false, true},
		{[]string{"-profile", "-version"}, false, false}, // a profile named -version
		{[]string{"htop", "-dir", "x"}, false, false},    // flags of the command, not ours
		{[]string{"--", "-version"}, false, false},
		{[]string{"-profile=bash", "-dir", "x"}, true, false},
	}
	for _, c := range cases {
		if got := hasFlag(c.args, "dir"); got != c.dir {
			t.Errorf("hasFlag(%q, dir) = %v", c.args, got)
		}
		if got := printsAndExits(c.args); got != c.prints {
			t.Errorf("printsAndExits(%q) = %v", c.args, got)
		}
	}
}

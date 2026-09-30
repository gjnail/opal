package jump

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		path  string
		words []string
		want  bool
	}{
		{"/home/u/src/opal", []string{"op"}, true},
		{"/home/u/src/opal", []string{"src", "op"}, true},
		{"/home/u/src/opal", []string{"op", "src"}, false}, // order matters
		{"/home/u/opal/docs", []string{"opal"}, false},     // last word must hit the basename
		{"/home/u/src/Opal", []string{"opal"}, true},       // smart case: lowercase is insensitive
		{"/home/u/src/opal", []string{"Opal"}, false},      // uppercase makes it sensitive
		{`C:\Users\u\Projects\Web`, []string{"proj", "web"}, true},
	}
	for _, c := range cases {
		if got := Match(c.path, c.words); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.path, c.words, got, c.want)
		}
	}
}

func TestAddRankQuery(t *testing.T) {
	t.Setenv("OPAL_DATA_DIR", t.TempDir())
	base := t.TempDir()
	a := filepath.Join(base, "work", "api")
	b := filepath.Join(base, "play", "apiary")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	for i := 0; i < 3; i++ {
		if err := Add(a); err != nil {
			t.Fatal(err)
		}
	}
	Add(b)
	if got, ok := Query([]string{"api"}, ""); !ok || got != a {
		t.Errorf("most frecent should win: got %q", got)
	}
	if got, _ := Query([]string{"api"}, a); got != b {
		t.Errorf("current dir is skipped: got %q", got)
	}
	os.RemoveAll(a)
	if got, _ := Query([]string{"api"}, ""); got != b {
		t.Errorf("deleted dirs are ignored: got %q", got)
	}
}

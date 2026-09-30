package complete

import "testing"

func TestFilter(t *testing.T) {
	in := []Candidate{{Value: "main"}, {Value: "Makefile"}, {Value: "feature/x"}, {Value: "main"}}
	got := Filter(in, "ma")
	if len(got) != 2 || got[0].Value != "main" || got[1].Value != "Makefile" {
		t.Errorf("smart-case prefix filter = %+v", got)
	}
	if got := Filter(in, "Ma"); len(got) != 1 || got[0].Value != "Makefile" {
		t.Errorf("uppercase makes it case-sensitive: %+v", got)
	}
	if got := Filter(Files, "x"); len(got) != 1 || got[0].Value != FilesSentinel {
		t.Error("the files sentinel must pass through")
	}
}

func TestGitSubcommandsAndOptions(t *testing.T) {
	got := Git(nil, "chec")
	if len(got) != 1 || got[0].Value != "checkout" {
		t.Errorf("git chec<Tab> = %+v", got)
	}
	got = Git([]string{"commit"}, "--am")
	if len(got) != 1 || got[0].Value != "--amend" {
		t.Errorf("git commit --am<Tab> = %+v", got)
	}
	got = Git([]string{"stash"}, "p")
	if len(got) != 2 || got[0].Value != "push" || got[1].Value != "pop" {
		t.Errorf("git stash p<Tab> = %+v", got)
	}
	if got := Git([]string{"checkout", "-b"}, ""); got != nil {
		t.Errorf("naming a new branch should offer nothing, got %+v", got)
	}
}

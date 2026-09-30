package fonts

import "github.com/rivo/uniseg"

func clusters(s string) []string {
	var out []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		out = append(out, g.Str())
	}
	return out
}

func wide(cluster string) bool { return uniseg.StringWidth(cluster) >= 2 }

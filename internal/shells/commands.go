package shells

import (
	"regexp"
	"strings"

	"opal/internal/config"
	"opal/internal/platform"
	"opal/internal/plugin"
)

// Commands lists the aliases and functions init defines in shell, for
// showing them outside it: Opal Terminal's command palette does. It's nil
// for a shell opal doesn't set up.
func Commands(shell string, cfg *config.Config, info platform.Info, has func(string) bool) []plugin.Def {
	shell = Normalize(shell)
	var name *regexp.Regexp
	switch shell {
	case "zsh", "bash":
		name = shName
	case "fish":
		name = fishName
	case "pwsh":
		name = psName
	default:
		return nil
	}
	res := plugin.Resolve(cfg, config.Target{Shell: shell, OS: info.OS, WSL: info.WSL}, has)
	var out []plugin.Def
	for _, d := range res.Defs {
		if !name.MatchString(d.Name) {
			continue
		}
		if shell == "pwsh" && !cfg.Shell.Pwsh.ClobberBuiltinAliases && psAliases[strings.ToLower(d.Name)] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// psAliases are the aliases PowerShell starts with. Init asks the shell
// itself which names are taken (Get-Alias) and leaves those alone; from
// outside the shell this list stands in for the question.
var psAliases = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`
		% ? ac cat cd chdir clc clear clhy cli clp cls clv cnsn compare copy cp cpi cpp cvpa
		dbp del diff dir dnsn ebp echo epal epcsv erase etsn exsn fc fhx fl foreach ft fw
		gal gbp gc gcb gci gcm gcs gdr gerr ghy gi gin gjb gl gm gmo gp gps gpv group gsn
		gsv gtz gu gv h history icm iex ihy ii ipal ipcsv ipmo irm iwr kill lp ls man md
		measure mi mount move mp mv nal ndr ni nmo nsn nv ogv oh popd ps pushd pwd r rbp
		rcjb rcsn rd rdr ren ri rjb rm rmdir rmo rni rnp rp rsn rv rvpa sajb sal saps sasv
		sbp scb select set shcm si sl sleep sls sort sp spjb spps spsv start stz sv tee
		type where wjb write
		asnp curl gsnp gwmi ise iwmi npssc rsnp rujb rwmi sc sujb swmi trcm wget`) {
		m[n] = true
	}
	return m
}()

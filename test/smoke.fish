# Smoke test: load opal into fish and check the essentials.
# Run with: fish --no-config -i test/smoke.fish
function fail
    echo "FAIL (fish $version): $argv" >&2
    exit 1
end
set -gx OPAL_DATA_DIR (mktemp -d) # keep test commands out of your real history

opal init fish | source; or fail "init did not evaluate"
test "$OPAL_SHELL" = fish; or fail "OPAL_SHELL not set"
abbr --list | string match -q gst; or fail "gst should be an abbreviation"
functions -q mkcd; or fail "mkcd should be a function"
functions -q j; or fail "j should be a function"

set -g _opal_ran 1
set -g _opal_cmd false
false
set -l out (fish_prompt | string collect)
string match -q '*✘ 1*' -- $out; or fail "failed status not shown: $out"
test (opal history list 1) = false; or fail "finished command not recorded in shared history"

set -l d (mktemp -d)
mkcd $d/a/b; and string match -q '*/a/b' -- $PWD; or fail "mkcd"
up 2; and test (realpath $PWD) = (realpath $d); or fail "up 2 landed in $PWD"
echo "ok: fish $version"
exit 0

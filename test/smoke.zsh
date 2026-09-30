# Smoke test: load opal into an interactive zsh and check the essentials.
# Run with: zsh -f -i test/smoke.zsh
fail() { print -u2 "FAIL (zsh $ZSH_VERSION): $*"; exit 1 }
export OPAL_COLOR=truecolor # CI terminals often claim no color support
export OPAL_DATA_DIR=$(mktemp -d) # keep test commands out of your real history
HISTFILE=$(mktemp) # and out of your shell history file

eval "$(opal init zsh)" || fail "init did not evaluate"
[[ $OPAL_SHELL == zsh ]] || fail "OPAL_SHELL not set"
(( $+aliases[gst] )) || fail "gst should be an alias"
(( $+functions[mkcd] )) || fail "mkcd should be a function"
(( $+functions[compdef] )) || fail "compinit did not run"
[[ $PROMPT == '${_opal_ps}' ]] || fail "PROMPT not installed"
[[ ${precmd_functions[1]} == _opal_precmd ]] || fail "precmd must run first"

_opal_start=$EPOCHREALTIME; _opal_cmd='false'; false; _opal_precmd
[[ $(opal history list 1) == false ]] || fail "finished command not recorded in shared history"
(( $+functions[_opal_suggest] )) || fail "built-in suggestions not loaded"
[[ $_opal_ps == *'%{'*'%}'* ]] || fail "zsh escapes not wrapped in %{ %}"
[[ $_opal_ps == *'✘ 1'* ]] || fail "failed status not shown"
print -P -- "$PROMPT" >/dev/null || fail "prompt does not expand"

# A directory name that looks like code must stay a name.
d=$(mktemp -d); mkdir -p "$d/\$(touch pwned)"; cd "$d/\$(touch pwned)" || fail "cd"
_opal_precmd; print -P -- "$PROMPT" >/dev/null 2>&1 # what zsh does when drawing it
[[ ! -e pwned && ! -e $d/pwned ]] || fail "prompt executed a directory name"

mkcd $d/a/b && [[ $PWD == */a/b ]] || fail "mkcd"
up 2 && [[ $PWD -ef $d ]] || fail "up 2 landed in $PWD"
print "ok: zsh $ZSH_VERSION"
exit 0

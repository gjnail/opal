# Smoke test: load opal into an interactive bash and check the essentials.
# Run with: bash --norc -i test/smoke.bash   (works on bash 3.2 through 5.x)
fail() { echo "FAIL (bash $BASH_VERSION): $*" >&2; exit 1; }
export OPAL_COLOR=truecolor # CI terminals often claim no color support
export OPAL_DATA_DIR=$(mktemp -d) # keep test commands out of your real history
HISTFILE=$(mktemp) # and out of your shell history file

eval "$(opal init bash)" || fail "init did not evaluate"
[[ $OPAL_SHELL == bash ]] || fail "OPAL_SHELL not set"
[[ $(type -t gst) == alias ]] || fail "gst should be an alias, type -t says: $(type -t gst)"
[[ $(type -t mkcd) == function ]] || fail "mkcd should be a function"
[[ $(type -t j) == function ]] || fail "j should be a function"
[[ $PS1 == '${_opal_ps}' ]] || fail "PS1 not installed"
[[ $PROMPT_COMMAND == _opal_precmd* ]] || fail "precmd must run first"
complete -p opal >/dev/null 2>&1 || fail "opal completion not registered"
COMP_WORDS=(opal theme set f); COMP_CWORD=3; _opal_complete
[[ ${COMPREPLY[*]} == fire ]] || fail "opal theme set f<Tab> gave: ${COMPREPLY[*]}"

# Simulate PS0 having stamped a start time, then a failing command.
if [[ -n ${EPOCHREALTIME-} ]]; then _opal_start=${EPOCHREALTIME//[^0-9]/}
elif (( BASH_VERSINFO[0] >= 4 )); then _opal_start=$SECONDS; fi
false; _opal_precmd
[[ $_opal_ps == *$'\001'* && $_opal_ps == *$'\002'* ]] || fail "prompt lacks readline ignore markers"
[[ $_opal_ps == *"✘ 1"* ]] || fail "failed status not shown: $_opal_ps"

# A directory name that looks like code must stay a name.
d=$(mktemp -d); mkdir -p "$d/\$(touch pwned)"; cd "$d/\$(touch pwned)" || fail "cd"
_opal_precmd
if (( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4) )); then
  eval 'x=${PS1@P}' # exactly the expansion bash applies when it draws PS1
fi
[[ ! -e pwned && ! -e "$d/pwned" ]] || fail "prompt executed a directory name"

mkcd "$d/a/b" && [[ $PWD == */a/b ]] || fail "mkcd"
up 2 && [[ $PWD -ef $d ]] || fail "up 2 landed in $PWD"
echo "ok: bash $BASH_VERSION"
exit 0

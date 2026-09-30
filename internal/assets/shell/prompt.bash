# opal: prompt hooks for bash
# (The init script sets _opal_hist and _opal_mark_c before this runs.)
_opal_start=''
_opal_ps=''
_opal_dir=''
_opal_ps0=''
_opal_jobs=''
_opal_hid=''
_opal_first=1

_opal_precmd() {
  local s=$? d=-1 rec= j=0 ran= h= hid=
  local -a hist=()
  history -a 2>/dev/null # share history with other sessions as you go
  if [[ -n $_opal_start ]]; then
    if [[ -n ${EPOCHREALTIME-} ]]; then
      local now=${EPOCHREALTIME//[^0-9]/}
      d=$(( (now - _opal_start) / 1000 ))
    else
      d=$(( (SECONDS - _opal_start) * 1000 ))
    fi
    _opal_start=''
    ran=1
  elif [[ -n $_opal_ps0 ]]; then
    s=0 # nothing ran (empty line, ^C): don't repeat the last error
  fi
  # Shared history: the command that just ran (bash 3.2 can't tell whether
  # one did, so the history number weeds out repeats there).
  if [[ -n $_opal_hist && -z $_opal_first && ( -n $ran || -z $_opal_ps0 ) ]]; then
    h=$(HISTTIMEFORMAT= builtin history 1)
    h=${h#"${h%%[![:space:]]*}"}
    hid=${h%%[[:space:]]*}
    h=${h#"$hid"}
    h=${h#"${h%%[![:space:]]*}"}
    if [[ -n $h && $hid != "$_opal_hid" ]]; then hist=(--cmd "$h" --cmd-id "$$:$hid"); fi
    _opal_hid=$hid
  fi
  _opal_first=
  if [[ -n $_opal_jobs ]]; then j='\j'; eval 'j=${j@P}'; fi
  if [[ $PWD != "$_opal_dir" ]]; then rec=--record; _opal_dir=$PWD; fi
  _opal_ps="$("$OPAL_BIN" prompt --shell bash --status "$s" --duration "$d" --jobs "$j" --width "${COLUMNS:-0}" $rec ${hist[@]+"${hist[@]}"})"
  return $s
}

# Command timing adapts to the bash you have: PS0 + EPOCHREALTIME on 5.x
# (microseconds, no fork), PS0 + SECONDS on 4.4, nothing on 3.2. PS0 also
# carries the "output starts here" mark for terminals that understand it.
if (( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4) )); then
  _opal_ps0=1
  _opal_jobs=1
  if [[ -n ${EPOCHREALTIME-} ]]; then
    PS0='${_opal_start:0:$((_opal_start=${EPOCHREALTIME//[^0-9]/},0))}${_opal_mark_c}'"${PS0-}"
  else
    PS0='${_opal_start:0:$((_opal_start=SECONDS,0))}${_opal_mark_c}'"${PS0-}"
  fi
fi

# Run first, so $? still belongs to the user's command. Prepending to element
# 0 works whether PROMPT_COMMAND is a string or a bash 5.1+ array.
if [[ ${PROMPT_COMMAND-} != *_opal_precmd* ]]; then
  PROMPT_COMMAND="_opal_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
shopt -s promptvars
# The rendered prompt lives in a variable: its contents are never re-expanded,
# so a directory named $(rm -rf ~) is just a name.
PS1='${_opal_ps}'

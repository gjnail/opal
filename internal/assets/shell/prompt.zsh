# opal: prompt hooks for zsh
# (The init script sets _opal_hist, _opal_tps and _opal_mark_c before this.)
zmodload zsh/datetime zsh/parameter 2>/dev/null
typeset -g _opal_start='' _opal_ps='' _opal_dir='' _opal_cmd=''

_opal_preexec() {
  _opal_start=$EPOCHREALTIME
  _opal_cmd=$1
  [[ -n $_opal_mark_c ]] && print -rn -- "$_opal_mark_c"
}

_opal_precmd() {
  local s=$? d=-1 rec=
  local -a hist
  if [[ -n $_opal_start ]]; then
    d=$(( (EPOCHREALTIME - _opal_start) * 1000 ))
    d=${d%%.*}
    _opal_start=''
    [[ -n $_opal_hist && -n $_opal_cmd ]] && hist=(--cmd "$_opal_cmd")
  else
    s=0 # nothing ran (empty line, ^C): don't repeat the last error
  fi
  _opal_cmd=''
  if [[ $PWD != "$_opal_dir" ]]; then rec=--record; _opal_dir=$PWD; fi
  _opal_ps="$("$OPAL_BIN" prompt --shell zsh --status $s --duration $d --jobs ${#jobstates} --width ${COLUMNS:-0} $rec "${hist[@]}")"
  PROMPT='${_opal_ps}' # put back after the transient prompt swapped it
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec _opal_preexec
# Run first, so $? still belongs to the user's command.
precmd_functions=(_opal_precmd ${precmd_functions:#_opal_precmd})
setopt prompt_subst prompt_percent
# The rendered prompt lives in a variable: its contents are never re-expanded,
# so a directory named $(rm -rf ~) is just a name.
PROMPT='${_opal_ps}'
RPROMPT=''

# Transient prompt: when a line is accepted, redraw its prompt as just the
# prompt character, so scrollback shows commands rather than repeated prompts.
if [[ -n $_opal_tps ]]; then
  _opal_line_finish() {
    PROMPT=$_opal_tps
    zle .reset-prompt 2>/dev/null
  }
  autoload -Uz add-zle-hook-widget 2>/dev/null && add-zle-hook-widget line-finish _opal_line_finish
fi

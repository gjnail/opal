# opal: as-you-type suggestions for zsh
# Grey as-you-type suggestions from history, like fish. If you load
# zsh-autosuggestions, it takes over and this steps aside; the accept keys
# below (Right arrow, End, Tab) work with either.
typeset -g _opal_sugg_hl=''

_opal_suggest_clear() {
  if [[ -n $_opal_sugg_hl ]]; then
    region_highlight=("${(@)region_highlight:#$_opal_sugg_hl}")
    _opal_sugg_hl=''
  fi
}

_opal_suggest() {
  (( $+functions[_zsh_autosuggest_start] )) && return
  _opal_suggest_clear
  POSTDISPLAY=''
  (( ${PENDING:-0} + ${KEYS_QUEUED_COUNT:-0} )) && return # typing or pasting fast
  [[ -z $BUFFER || $CURSOR -ne ${#BUFFER} ]] && return
  # $history is newest-first; (b) makes the typed text match literally.
  local match=${history[(r)${(b)BUFFER}?*]}
  [[ -z $match || $match == *$'\n'* ]] && return
  POSTDISPLAY=${match#$BUFFER}
  _opal_sugg_hl="${#BUFFER} $(( ${#BUFFER} + ${#POSTDISPLAY} )) ${_opal_sugg_style:-fg=8}"
  region_highlight+=("$_opal_sugg_hl")
}

_opal_suggest_finish() {
  (( $+functions[_zsh_autosuggest_start] )) && return
  _opal_suggest_clear
  POSTDISPLAY=''
}

# Accept with Right arrow or End at the end of the line (Tab is handled in core).
zle-opal-forward() {
  if [[ -n $POSTDISPLAY && $CURSOR -eq ${#BUFFER} ]]; then
    BUFFER+=$POSTDISPLAY; POSTDISPLAY=''; CURSOR=${#BUFFER}
  else
    zle .forward-char
  fi
}
zle-opal-end() {
  if [[ -n $POSTDISPLAY && $CURSOR -eq ${#BUFFER} ]]; then
    BUFFER+=$POSTDISPLAY; POSTDISPLAY=''; CURSOR=${#BUFFER}
  else
    zle .end-of-line
  fi
}
zle -N zle-opal-forward
zle -N zle-opal-end

if autoload -Uz add-zle-hook-widget 2>/dev/null; then
  add-zle-hook-widget line-pre-redraw _opal_suggest
  add-zle-hook-widget line-finish _opal_suggest_finish
  () {
    local km
    for km in emacs viins; do
      bindkey -M $km '^[[C' zle-opal-forward
      bindkey -M $km '^[OC' zle-opal-forward
      bindkey -M $km '^[[F' zle-opal-end
      bindkey -M $km '^[OF' zle-opal-end
    done
    bindkey -M emacs '^F' zle-opal-forward
    bindkey -M emacs '^E' zle-opal-end
  }
fi

# opal: tab completion for zsh
# zsh already completes git (and expands aliases like gco before completing),
# so opal only teaches it about its own commands and jump directories.
_opal_complete() {
  local -a out
  out=("${(@f)$(opal complete --shell zsh --cur="$PREFIX" "${(@)words[1,CURRENT-1]}" 2>/dev/null)}")
  if [[ ${out[1]} == __files__ ]]; then _files; return; fi
  [[ -n ${out[1]} ]] || return 1
  if [[ ${words[1]} == j ]]; then
    compadd -U -- "${(@)out%%:*}" # directories replace the typed word
  else
    _describe -t opal 'opal' out
  fi
}
(( $+functions[compdef] )) && compdef _opal_complete @@NAMES@@

# opal: tab completion for bash
_opal_complete() {
  # Copy the words before IFS changes: with IFS set to a newline, bash 3.2
  # doesn't pass a quoted array slice on as separate words.
  local cur=${COMP_WORDS[COMP_CWORD]} words
  words=("${COMP_WORDS[@]:0:COMP_CWORD}")
  local IFS=$'\n'
  COMPREPLY=($(opal complete --shell bash --cur="$cur" "${words[@]}" 2>/dev/null))
  # __files__ (or nothing) means: let readline complete paths (-o default).
  [[ ${COMPREPLY[0]-} == __files__ ]] && COMPREPLY=()
  return 0
}
complete -o default -F _opal_complete @@NAMES@@

# Git aliases complete like the git subcommand they stand for. Prefer git's
# own completion (loading it if bash-completion would only do so lazily),
# otherwise fall back to opal's.
if [[ -n "@@GITALIASES@@" ]]; then
  if ! declare -F __git_complete >/dev/null; then
    for __opal_f in /usr/share/bash-completion/completions/git /usr/share/git/completion/git-completion.bash \
        /mingw64/share/git/completion/git-completion.bash /opt/homebrew/etc/bash_completion.d/git-completion.bash \
        /usr/local/etc/bash_completion.d/git-completion.bash \
        /Library/Developer/CommandLineTools/usr/share/git-core/git-completion.bash; do
      if [[ -r $__opal_f ]]; then . "$__opal_f" 2>/dev/null; break; fi
    done
    unset __opal_f
  fi
  _opal_gitcomp() {
    local fn="_git_${2//-/_}"
    [[ $2 == __main ]] && fn=__git_main
    if declare -F __git_complete >/dev/null && declare -F "$fn" >/dev/null; then
      __git_complete "$1" "$fn"
    else
      complete -o default -F _opal_complete "$1"
    fi
  }
  for __opal_p in @@GITALIASES@@; do _opal_gitcomp "${__opal_p%%=*}" "${__opal_p#*=}"; done
  unset __opal_p
fi

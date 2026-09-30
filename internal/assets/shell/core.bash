# opal: default settings for bash (3.2 through 5.x)
# History: appended not overwritten, large, no dupes, space-prefixed lines stay private.
shopt -s histappend cmdhist checkwinsize
HISTCONTROL=ignoreboth:erasedups
(( ${HISTSIZE:-0} >= 50000 )) || HISTSIZE=50000
(( ${HISTFILESIZE:-0} >= 100000 )) || HISTFILESIZE=100000
: "${HISTTIMEFORMAT:=%F %T  }"
shopt -s cdspell no_empty_cmd_completion 2>/dev/null
# bash 4+ only: type a path to cd there, ** globs, fix typos in dir names.
if (( BASH_VERSINFO[0] >= 4 )); then shopt -s autocd dirspell globstar 2>/dev/null; fi

# Completion: load bash-completion from wherever this OS keeps it.
if [[ -z ${BASH_COMPLETION_VERSINFO-} ]] && ! shopt -oq posix; then
  for __opal_f in /usr/share/bash-completion/bash_completion /etc/bash_completion \
      /opt/homebrew/etc/profile.d/bash_completion.sh /usr/local/etc/profile.d/bash_completion.sh \
      /usr/local/etc/bash_completion "${PREFIX:-/nonexistent}/share/bash-completion/bash_completion"; do
    if [[ -r $__opal_f ]]; then . "$__opal_f"; break; fi
  done
  unset __opal_f
fi

# Readline: case-insensitive, colored completion; Up/Down search history for what's typed.
if [[ $- == *i* ]]; then
  bind 'set completion-ignore-case on' 2>/dev/null
  bind 'set completion-map-case on' 2>/dev/null
  bind 'set show-all-if-ambiguous on' 2>/dev/null
  bind 'set mark-symlinked-directories on' 2>/dev/null
  bind 'set colored-stats on' 2>/dev/null
  bind 'set colored-completion-prefix on' 2>/dev/null
  bind '"\e[A": history-search-backward' 2>/dev/null
  bind '"\e[B": history-search-forward' 2>/dev/null
  bind '"\eOA": history-search-backward' 2>/dev/null
  bind '"\eOB": history-search-forward' 2>/dev/null
  bind '"\e[1;5C": forward-word' 2>/dev/null
  bind '"\e[1;5D": backward-word' 2>/dev/null
  # Esc Esc puts sudo in front of the current line.
  bind '"\e\e": "\C-asudo \C-e"' 2>/dev/null
fi

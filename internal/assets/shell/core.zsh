# opal: default settings for zsh
# History: large, shared across sessions, no dupes, space-prefixed lines stay private.
[[ -n $HISTFILE ]] || HISTFILE=${ZDOTDIR:-$HOME}/.zsh_history
(( HISTSIZE >= 50000 )) || HISTSIZE=50000
(( SAVEHIST >= 50000 )) || SAVEHIST=50000
setopt extended_history hist_expire_dups_first hist_ignore_dups hist_ignore_space hist_verify share_history
# Directories: type a path to cd there; cd keeps a stack (try cd -<TAB>).
setopt auto_cd auto_pushd pushd_ignore_dups pushd_minus pushd_silent
setopt interactive_comments no_beep complete_in_word always_to_end

# Completion: menu select, case-insensitive + partial-word matching, cached dump.
zmodload -i zsh/complist 2>/dev/null
if (( ! $+functions[compdef] )); then
  autoload -Uz compinit
  () {
    setopt local_options extended_glob
    local dump=${ZDOTDIR:-$HOME}/.zcompdump
    # Full security check at most once a day; otherwise trust the dump (fast).
    if [[ ! -s $dump || -n $dump(#qN.mh+24) ]]; then
      compinit -i -d "$dump"
    else
      compinit -C -d "$dump"
    fi
  }
fi
zstyle ':completion:*' menu select
zstyle ':completion:*' matcher-list 'm:{a-zA-Z}={A-Za-z}' 'r:|[._-]=* r:|=*' 'l:|=* r:|=*'
zstyle ':completion:*' list-colors "${(s.:.)LS_COLORS}"
zstyle ':completion:*' group-name ''
zstyle ':completion:*' use-cache on
zstyle ':completion:*' cache-path "${XDG_CACHE_HOME:-$HOME/.cache}/opal/zcompcache"
zstyle ':completion:*:descriptions' format '%F{8}── %d ──%f'
zstyle ':completion:*:warnings' format '%F{8}── no matches ──%f'

# Keys: emacs mode unless you picked vi; Up/Down search history for what's typed.
[[ $(bindkey -lL main) == *viins* ]] || bindkey -e
autoload -Uz up-line-or-beginning-search down-line-or-beginning-search edit-command-line
zle -N up-line-or-beginning-search
zle -N down-line-or-beginning-search
zle -N edit-command-line

# Esc Esc toggles sudo on the current (or previous) command line.
_opal_sudo() {
  [[ -z $BUFFER ]] && BUFFER=$(fc -ln -1)
  if [[ $BUFFER == sudo\ * ]]; then BUFFER=${BUFFER#sudo }; else BUFFER="sudo $BUFFER"; fi
  CURSOR=${#BUFFER}
}
zle -N _opal_sudo

() {
  local km
  for km in emacs viins; do
    bindkey -M $km '^[[A' up-line-or-beginning-search
    bindkey -M $km '^[OA' up-line-or-beginning-search
    bindkey -M $km '^[[B' down-line-or-beginning-search
    bindkey -M $km '^[OB' down-line-or-beginning-search
    bindkey -M $km '^[[H' beginning-of-line
    bindkey -M $km '^[OH' beginning-of-line
    bindkey -M $km '^[[1~' beginning-of-line
    bindkey -M $km '^[[F' end-of-line
    bindkey -M $km '^[OF' end-of-line
    bindkey -M $km '^[[4~' end-of-line
    bindkey -M $km '^[[3~' delete-char
    bindkey -M $km '^[[1;5C' forward-word
    bindkey -M $km '^[[1;5D' backward-word
    bindkey -M $km '^[[Z' reverse-menu-complete
    bindkey -M $km '^X^E' edit-command-line
  done
  bindkey -M emacs '\e\e' _opal_sudo
  bindkey -M vicmd '\e\e' _opal_sudo
}

# Tab takes the grey suggestion when zsh-autosuggestions is showing one, and
# otherwise does whatever Tab did before (plain completion, fzf-tab, ...).
# The zle- prefix matters: zsh-autosuggestions leaves zle-* widgets unwrapped,
# so this one sees the suggestion and decides for itself.
typeset -g _opal_tab_prev=${${(z)$(bindkey '^I')}[2]:-expand-or-complete}
[[ $_opal_tab_prev == zle-opal-tab ]] && _opal_tab_prev=expand-or-complete
zle-opal-tab() {
  if [[ -n $POSTDISPLAY && $CURSOR -eq ${#BUFFER} ]]; then
    BUFFER+=$POSTDISPLAY; POSTDISPLAY=''; CURSOR=${#BUFFER} # opal's or zsh-autosuggestions'
  else
    zle $_opal_tab_prev
  fi
}
zle -N zle-opal-tab
bindkey -M emacs '^I' zle-opal-tab
bindkey -M viins '^I' zle-opal-tab

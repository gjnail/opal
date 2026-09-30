# opal: Ctrl+R history search for zsh
# Ctrl+R: fuzzy-search the history shared by every shell you use.
_opal_history_widget() {
  local sel
  sel=$("$OPAL_BIN" history search --shell zsh --query "$BUFFER" 2>/dev/null) && {
    BUFFER=$sel
    CURSOR=${#BUFFER}
  }
  zle reset-prompt
}
zle -N _opal_history_widget
bindkey -M emacs '^R' _opal_history_widget
bindkey -M viins '^R' _opal_history_widget

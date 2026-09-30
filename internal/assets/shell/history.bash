# opal: Ctrl+R history search for bash
# Ctrl+R: fuzzy-search the history shared by every shell you use.
# (bind -x needs bash 4; 3.2 keeps readline's own Ctrl+R.)
if (( BASH_VERSINFO[0] >= 4 )) && [[ $- == *i* ]]; then
  _opal_history_widget() {
    local sel
    sel=$("$OPAL_BIN" history search --shell bash --query "$READLINE_LINE" 2>/dev/null) || return 0
    READLINE_LINE=$sel
    READLINE_POINT=${#sel}
  }
  bind -x '"\C-r": _opal_history_widget' 2>/dev/null
fi

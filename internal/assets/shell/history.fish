# opal: Ctrl+R history search for fish
# Ctrl+R: fuzzy-search the history shared by every shell you use.
function __opal_history
    set -l sel (command $OPAL_BIN history search --shell fish --query (commandline) 2>/dev/null | string collect)
    and commandline -r -- $sel
    commandline -f repaint
end
bind \cr __opal_history
bind -M insert \cr __opal_history 2>/dev/null

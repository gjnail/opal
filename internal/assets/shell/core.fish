# opal: default settings for fish
# fish already has autosuggestions, syntax highlighting, ll/la and Alt+S
# for sudo, so opal changes little here.
set -q fish_color_valid_path; or set -g fish_color_valid_path --underline

# Tab takes the grey autosuggestion when one is showing, and completes
# otherwise (Right arrow still accepts too).
function __opal_tab
    if commandline --showing-suggestion 2>/dev/null
        commandline -f accept-autosuggestion
    else
        commandline -f complete
    end
end
bind \t __opal_tab
bind -M insert \t __opal_tab 2>/dev/null

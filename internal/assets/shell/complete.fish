# opal: tab completion for fish
# fish completes git itself, and abbreviations expand before you press Tab,
# so opal only teaches it about its own commands and jump directories.
function __opal_complete
    set -l cur (commandline -ct)
    set -l out (opal complete --shell fish --cur=$cur (commandline -opc) 2>/dev/null)
    if test "$out[1]" = __files__
        __fish_complete_path $cur
        return
    end
    printf '%s\n' $out
end
for __opal_c in @@NAMES@@
    complete -c $__opal_c -f -a '(__opal_complete)'
end
set -e __opal_c

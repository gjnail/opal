# opal: prompt hooks for fish
# (The init script sets _opal_hist, _opal_tps and _opal_mark_c before this.)
set -g _opal_ran 0
set -g _opal_dir ''
set -g _opal_cmd ''

function _opal_preexec --on-event fish_preexec
    test -n "$_opal_mark_c"; and printf '%s' $_opal_mark_c
end

function _opal_postexec --on-event fish_postexec
    set -g _opal_ran 1
    set -g _opal_cmd $argv[1]
end

function fish_prompt
    set -l s $status
    if set -q _opal_transient
        set -e _opal_transient
        printf '%s' $_opal_tps
        return
    end
    set -l d -1
    set -l hist
    if test $_opal_ran = 1
        set d $CMD_DURATION
        set -g _opal_ran 0
        if test -n "$_opal_hist" -a -n "$_opal_cmd"
            set hist --cmd $_opal_cmd
        end
    else
        set s 0 # nothing ran (empty line, ^C): don't repeat the last error
    end
    set -g _opal_cmd ''
    set -l rec
    if test "$PWD" != "$_opal_dir"
        set rec --record
        set -g _opal_dir $PWD
    end
    set -l j 0
    if jobs -q
        set j (count (jobs -p))
    end
    command $OPAL_BIN prompt --shell fish --status $s --duration $d --jobs $j --width $COLUMNS $rec $hist
end

function fish_right_prompt
end

# Transient prompt: redraw an accepted line's prompt as just the prompt character.
if test -n "$_opal_tps"
    function __opal_enter
        if commandline --is-valid
            set -g _opal_transient 1
            commandline -f repaint
        end
        commandline -f execute
    end
    bind \r __opal_enter
    bind -M insert \r __opal_enter 2>/dev/null
end

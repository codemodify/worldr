# Optional, source this file in an interactive Bash 4.4+ session.
# No commands are replaced or replayed. PS0 emits a boundary immediately before
# execution; existing PS0, PS1 and PROMPT_COMMAND hooks are retained.
if [[ $- == *i* && -z ${_WORLDR_SHELL_INTEGRATION-} ]]; then
    if (( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4) )); then
        _WORLDR_SHELL_INTEGRATION=1
        _worldr_prompt_boundary() {
            local status=$?
            printf '\033]133;D;%d\007\033]133;A\007' "$status"
            return "$status"
        }
        PS0=$'\033]133;C\007'${PS0-}
        PS1=${PS1-}'\[\e]133;B\a\]'
        if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == 'declare -a '* ]]; then
            PROMPT_COMMAND=(_worldr_prompt_boundary "${PROMPT_COMMAND[@]}")
        else
            PROMPT_COMMAND="_worldr_prompt_boundary${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
        fi
    fi
fi

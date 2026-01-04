package yascli

import "fmt"

type completionBashCmd struct{}

func (c *completionBashCmd) SkipRepoCheck() bool {
	return true
}

func (c *completionBashCmd) Execute(args []string) error {
	completionScript := `# bash completion for yas
_yas_completion() {
    local cur prev words cword
    _init_completion || return

    # Commands
    local commands="abort add annotate branch config continue delete hook init list merge move submit refresh restack state sync completion"

    # Check if we're completing a command
    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$commands" -- "$cur"))
        return 0
    fi

    # Get the command being used
    local cmd="${words[1]}"

    # Commands that accept branch names as arguments
    case "$cmd" in
        add|delete|move)
            # Complete with git branch names
            if command -v git &> /dev/null && git rev-parse --git-dir &> /dev/null 2>&1; then
                local branches=$(git for-each-ref --format='%(refname:short)' refs/heads/ 2>/dev/null)
                COMPREPLY=($(compgen -W "$branches" -- "$cur"))
            fi
            ;;
        hook)
            # Complete hook subcommands
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "bash zsh" -- "$cur"))
            fi
            ;;
        completion)
            # Complete completion subcommands
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur"))
            fi
            ;;
        config)
            # Complete config subcommands
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "show set" -- "$cur"))
            fi
            ;;
        state)
            # Complete state subcommands
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "show" -- "$cur"))
            fi
            ;;
        branch)
            # Complete branch subcommands
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=($(compgen -W "new" -- "$cur"))
            fi
            ;;
    esac
}

complete -F _yas_completion yas
`

	fmt.Println(completionScript)

	return nil
}

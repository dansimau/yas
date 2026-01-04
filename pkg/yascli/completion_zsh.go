package yascli

import "fmt"

type completionZshCmd struct{}

func (c *completionZshCmd) SkipRepoCheck() bool {
	return true
}

func (c *completionZshCmd) Execute(args []string) error {
	completionScript := `#compdef yas

_yas() {
    local -a commands
    commands=(
        'abort:Abort a restack operation in progress'
        'add:Add/set parent of branch'
        'annotate:Annotate PR with stack information'
        'branch:Work with branches'
        'config:Manage repository-specific configuration'
        'continue:Continue a restack operation after fixing conflicts'
        'delete:Delete a branch and its worktree'
        'hook:Print shell integration hook for bash or zsh'
        'init:Set up initial configuration'
        'list:List stacks'
        'merge:Merge PR for current branch'
        'move:Move current branch and descendants to a new parent'
        'submit:Push to remote and open or update PR(s)'
        'refresh:Refresh remote status for current branch'
        'restack:Rebase all branches in the current stack'
        'state:Manage branch state'
        'sync:Pull latest PR statuses and sync with local branches'
        'completion:Generate shell completion script'
    )

    _arguments -C \
        '(-h --help)'{-h,--help}'[Show help message]' \
        '(-v --verbose)'{-v,--verbose}'[Verbose output]' \
        '(-r --repo)'{-r,--repo}'[Repo directory]:directory:_files -/' \
        '--dry-run[Do not make any changes, just show what will happen]' \
        '1: :->command' \
        '*::arg:->args'

    case $state in
        command)
            _describe 'command' commands
            ;;
        args)
            case $words[1] in
                add|delete|move)
                    # Complete with git branches
                    if command -v git &> /dev/null && git rev-parse --git-dir &> /dev/null 2>&1; then
                        local -a branches
                        branches=(${(f)"$(git for-each-ref --format='%(refname:short)' refs/heads/ 2>/dev/null)"})
                        _describe 'branch' branches
                    fi
                    ;;
                hook)
                    local -a hook_cmds
                    hook_cmds=(
                        'bash:Print bash shell hook'
                        'zsh:Print zsh shell hook'
                    )
                    _describe 'hook command' hook_cmds
                    ;;
                completion)
                    local -a completion_cmds
                    completion_cmds=(
                        'bash:Generate bash completion script'
                        'zsh:Generate zsh completion script'
                        'fish:Generate fish completion script'
                    )
                    _describe 'completion command' completion_cmds
                    ;;
                config)
                    local -a config_cmds
                    config_cmds=(
                        'show:Show current configuration'
                        'set:Set configuration value'
                    )
                    _describe 'config command' config_cmds
                    ;;
                state)
                    local -a state_cmds
                    state_cmds=(
                        'show:Show branch state'
                    )
                    _describe 'state command' state_cmds
                    ;;
                branch)
                    local -a branch_cmds
                    branch_cmds=(
                        'new:Create a new branch'
                    )
                    _describe 'branch command' branch_cmds
                    ;;
            esac
            ;;
    esac
}

_yas "$@"
`

	fmt.Println(completionScript)

	return nil
}

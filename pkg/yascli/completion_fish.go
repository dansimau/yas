package yascli

import "fmt"

type completionFishCmd struct{}

func (c *completionFishCmd) SkipRepoCheck() bool {
	return true
}

func (c *completionFishCmd) Execute(args []string) error {
	completionScript := `# fish completion for yas

# Helper function to get git branches
function __yas_git_branches
    if command -v git >/dev/null 2>&1; and git rev-parse --git-dir >/dev/null 2>&1
        git for-each-ref --format='%(refname:short)' refs/heads/ 2>/dev/null
    end
end

# Disable file completion by default
complete -c yas -f

# Global options
complete -c yas -l help -s h -d 'Show help message'
complete -c yas -l verbose -s v -d 'Verbose output'
complete -c yas -l repo -s r -d 'Repo directory' -r -F
complete -c yas -l dry-run -d 'Do not make any changes, just show what will happen'

# Commands
complete -c yas -n '__fish_use_subcommand' -a 'abort' -d 'Abort a restack operation in progress'
complete -c yas -n '__fish_use_subcommand' -a 'add' -d 'Add/set parent of branch'
complete -c yas -n '__fish_use_subcommand' -a 'annotate' -d 'Annotate PR with stack information'
complete -c yas -n '__fish_use_subcommand' -a 'branch' -d 'Work with branches'
complete -c yas -n '__fish_use_subcommand' -a 'config' -d 'Manage repository-specific configuration'
complete -c yas -n '__fish_use_subcommand' -a 'continue' -d 'Continue a restack operation after fixing conflicts'
complete -c yas -n '__fish_use_subcommand' -a 'delete' -d 'Delete a branch and its worktree'
complete -c yas -n '__fish_use_subcommand' -a 'hook' -d 'Print shell integration hook for bash or zsh'
complete -c yas -n '__fish_use_subcommand' -a 'init' -d 'Set up initial configuration'
complete -c yas -n '__fish_use_subcommand' -a 'list' -d 'List stacks'
complete -c yas -n '__fish_use_subcommand' -a 'merge' -d 'Merge PR for current branch'
complete -c yas -n '__fish_use_subcommand' -a 'move' -d 'Move current branch and descendants to a new parent'
complete -c yas -n '__fish_use_subcommand' -a 'submit' -d 'Push to remote and open or update PR(s)'
complete -c yas -n '__fish_use_subcommand' -a 'refresh' -d 'Refresh remote status for current branch'
complete -c yas -n '__fish_use_subcommand' -a 'restack' -d 'Rebase all branches in the current stack'
complete -c yas -n '__fish_use_subcommand' -a 'state' -d 'Manage branch state'
complete -c yas -n '__fish_use_subcommand' -a 'sync' -d 'Pull latest PR statuses and sync with local branches'
complete -c yas -n '__fish_use_subcommand' -a 'completion' -d 'Generate shell completion script'

# Branch completions for commands that accept branch names
complete -c yas -n '__fish_seen_subcommand_from add' -a '(__yas_git_branches)' -d 'Branch name'
complete -c yas -n '__fish_seen_subcommand_from delete' -a '(__yas_git_branches)' -d 'Branch name'
complete -c yas -n '__fish_seen_subcommand_from move' -a '(__yas_git_branches)' -d 'Branch name'

# Subcommands for hook
complete -c yas -n '__fish_seen_subcommand_from hook; and not __fish_seen_subcommand_from bash zsh' -a 'bash' -d 'Print bash shell hook'
complete -c yas -n '__fish_seen_subcommand_from hook; and not __fish_seen_subcommand_from bash zsh' -a 'zsh' -d 'Print zsh shell hook'

# Subcommands for completion
complete -c yas -n '__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish' -a 'bash' -d 'Generate bash completion script'
complete -c yas -n '__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish' -a 'zsh' -d 'Generate zsh completion script'
complete -c yas -n '__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish' -a 'fish' -d 'Generate fish completion script'

# Subcommands for config
complete -c yas -n '__fish_seen_subcommand_from config; and not __fish_seen_subcommand_from show set' -a 'show' -d 'Show current configuration'
complete -c yas -n '__fish_seen_subcommand_from config; and not __fish_seen_subcommand_from show set' -a 'set' -d 'Set configuration value'

# Subcommands for state
complete -c yas -n '__fish_seen_subcommand_from state; and not __fish_seen_subcommand_from show' -a 'show' -d 'Show branch state'

# Subcommands for branch
complete -c yas -n '__fish_seen_subcommand_from branch; and not __fish_seen_subcommand_from new' -a 'new' -d 'Create a new branch'
`

	fmt.Println(completionScript)

	return nil
}

package yascli

import "fmt"

type hookZshCmd struct{}

func (c *hookZshCmd) SkipRepoCheck() bool {
	return true
}

func (c *hookZshCmd) Execute(args []string) error {
	hookScript := `
# yas shell hook for zsh
yas() {
	local yas_shell_exec_file
	yas_shell_exec_file="$(mktemp)"

	export YAS_SHELL_EXEC="$yas_shell_exec_file"

	command yas "$@" || return $?

	if [ -s "$yas_shell_exec_file" ]; then
		source "$yas_shell_exec_file" || return $?
	fi

	rm -f "$yas_shell_exec_file"
	unset YAS_SHELL_EXEC
}`

	fmt.Println(hookScript)

	return nil
}

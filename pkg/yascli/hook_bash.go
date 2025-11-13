package yascli

import "fmt"

type hookBashCmd struct{}

func (c *hookBashCmd) SkipRepoCheck() bool {
	return true
}

func (c *hookBashCmd) Execute(args []string) error {
	hookScript := `
# yas shell hook for bash
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

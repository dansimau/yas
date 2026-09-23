// Package shellhook lets a command change its parent shell's state (such as
// the working directory), which a child process cannot do directly.
//
// The shell hook wraps the command in a shell function that points an
// environment variable at a temporary file, runs the command, and then
// sources whatever the command wrote to that file.
package shellhook

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"al.essio.dev/pkg/shellescape"
)

// Hook is the shell hook of one command.
type Hook struct {
	// Command is the program the hook wraps; it prints the hook with
	// "<Command> hook bash|zsh".
	Command string
	// EnvVar names the file the hook sources after the command exits.
	EnvVar string
}

// ErrIfNotInstalled returns an error explaining how to install the hook unless
// the command was run through it.
func (h Hook) ErrIfNotInstalled() error {
	if os.Getenv(h.EnvVar) != "" {
		return nil
	}

	return errors.New(h.EnvVar + " environment variable not set\n\n" +
		"To enable directory switching, install the " + h.Command + " shell hook:\n\n" +
		"  # For bash, add to ~/.bashrc:\n" +
		"  eval \"$(" + h.Command + " hook bash)\"\n\n" +
		"  # For zsh, add to ~/.zshrc:\n" +
		"  eval \"$(" + h.Command + " hook zsh)\"")
}

// Script returns the hook for shell ("bash" or "zsh").
func (h Hook) Script(shell string) string {
	return fmt.Sprintf(`
# %[1]s shell hook for %[2]s
%[1]s() {
	local %[1]s_shell_exec_file
	%[1]s_shell_exec_file="$(mktemp)"

	export %[3]s="$%[1]s_shell_exec_file"

	command %[1]s "$@" || return $?

	if [ -s "$%[1]s_shell_exec_file" ]; then
		source "$%[1]s_shell_exec_file" || return $?
	fi

	rm -f "$%[1]s_shell_exec_file"
	unset %[3]s
}`, h.Command, shell, h.EnvVar)
}

// NewWriter opens the file named by the hook's environment variable for
// appending commands. It fails if the hook is not installed.
func (h Hook) NewWriter() (*Writer, error) {
	if err := h.ErrIfNotInstalled(); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(os.Getenv(h.EnvVar), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s file: %w", h.EnvVar, err)
	}

	return &Writer{file: file}, nil
}

// Writer appends shell commands to the file the hook will source.
type Writer struct {
	file *os.File
}

// WriteCommand writes a shell command with properly escaped arguments.
func (w *Writer) WriteCommand(command string, args ...string) error {
	cmdLine := []string{command}
	for _, arg := range args {
		cmdLine = append(cmdLine, shellescape.Quote(arg))
	}

	_, err := w.file.WriteString(strings.Join(cmdLine, " ") + "\n")

	return err
}

// Close closes the file handle.
func (w *Writer) Close() error {
	return w.file.Close()
}

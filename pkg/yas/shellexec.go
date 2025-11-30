package yas

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"al.essio.dev/pkg/shellescape"
)

var ErrShellHookNotInstalled = errors.New("YAS_SHELL_EXEC environment variable not set\n\nTo enable worktree directory switching, install the yas shell hook:\n\n  # For bash, add to ~/.bashrc:\n  eval \"$(yas hook bash)\"\n\n  # For zsh, add to ~/.zshrc:\n  eval \"$(yas hook zsh)\"")

func errIfShellHookNotInstalled() error {
	if os.Getenv("YAS_SHELL_EXEC") == "" {
		return ErrShellHookNotInstalled
	}

	return nil
}

// ShellExecWriter writes shell commands to the YAS_SHELL_EXEC file.
type ShellExecWriter struct {
	filePath string
	file     *os.File
}

// NewShellExecWriter creates a new ShellExecWriter
// Returns an error if YAS_SHELL_EXEC environment variable is not set.
func NewShellExecWriter() (*ShellExecWriter, error) {
	filePath := os.Getenv("YAS_SHELL_EXEC")
	if filePath == "" {
		return nil, ErrShellHookNotInstalled
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open YAS_SHELL_EXEC file: %w", err)
	}

	return &ShellExecWriter{
		filePath: filePath,
		file:     file,
	}, nil
}

// WriteCommand writes a shell command with properly escaped arguments.
func (w *ShellExecWriter) WriteCommand(command string, args ...string) error {
	escapedArgs := make([]string, len(args))
	for i, arg := range args {
		escapedArgs[i] = shellescape.Quote(arg)
	}

	cmdLine := []string{command}
	if len(escapedArgs) > 0 {
		cmdLine = append(cmdLine, escapedArgs...)
	}

	_, err := w.file.WriteString(strings.Join(cmdLine, " ") + "\n")

	return err
}

// Close closes the file handle.
func (w *ShellExecWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}

	return nil
}

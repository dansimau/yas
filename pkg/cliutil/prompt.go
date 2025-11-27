// Package cliutil provides utilities for the command-line interface.
package cliutil

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrAbort = errors.New("Aborting")

type PromptOptions struct {
	Text      string
	Default   string
	Validator func(input string) error
}

func Prompt(opts PromptOptions) string {
Prompt:
	scanner := bufio.NewScanner(os.Stdin)

	if opts.Text != "" {
		fmt.Fprint(os.Stderr, opts.Text+" ")
	}

	if opts.Default != "" {
		fmt.Fprintf(os.Stderr, "[%s] ", opts.Default)
	}

	scanner.Scan()

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	input := strings.TrimSpace(scanner.Text())

	// Apply default value if input is empty
	if input == "" && opts.Default != "" {
		input = opts.Default
	}

	if opts.Validator != nil {
		if err := opts.Validator(input); err != nil {
			if errors.Is(err, ErrAbort) {
				os.Exit(1)
			}

			fmt.Fprintln(os.Stderr, err)

			goto Prompt
		}
	}

	return input
}

// Confirm prompts the user for a yes/no confirmation.
func Confirm(prompt string) bool {
	switch Prompt(PromptOptions{
		Text: prompt,
	}) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	}

	return false
}

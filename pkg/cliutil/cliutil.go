// Package cliutil provides utilities for the command-line interface.
package cliutil

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

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
			fmt.Fprintln(os.Stderr, err)

			goto Prompt
		}
	}

	return input
}

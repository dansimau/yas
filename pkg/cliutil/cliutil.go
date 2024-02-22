package cliutil

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"golang.org/x/term"
)

func PrintTable(rows [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeader(rows[0])
	table.SetBorder(false)

	for _, row := range rows[1:] {
		table.Append(row)
	}

	table.Render()
}

// PromptWithValidation prompts the user for input and returns the result.
// Before returning, it runs the specified validator. If the validator fails,
// it outputs the error to the user and repeats the input prompt until the
// input is valid.
func PromptWithValidation(text string, validator func(input string) error) string {
Prompt:
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(os.Stderr, text+" ")
	scanner.Scan()
	if err := scanner.Err(); err != nil {
		panic(err)
	}

	input := scanner.Text()

	if err := validator(input); err != nil {
		fmt.Fprintln(os.Stderr, err)
		goto Prompt
	}

	return input
}

// ReadSecretInputFromTerminal disables echoing and reads input interactively.
func ReadSecretInputFromTerminal(in *os.File) (string, error) {
	b, err := term.ReadPassword(int(in.Fd()))
	return string(b), err
}

// StdinIsPipe returns whether or not stdin is a pipe.
func StdinIsPipe() bool {
	fi, _ := os.Stdin.Stat() // get the FileInfo struct describing the standard input.
	return (fi.Mode() & os.ModeCharDevice) == 0
}

// PrintVerbose prints the specified message if verbose is true.
func PrintVerbose(verbose bool, text string) {
	if verbose {
		fmt.Println(text)
	}
}

// parseConfirmationInput takes a string as input and returns whether or not
// the input is a "yes" or a "no". If the input is empty, it returns
// defaultIfEmpty. If the input cannot be parsed, an error is returned.
func parseConfirmationInput(input string, defaultIfEmpty bool) (bool, error) {
	switch strings.TrimSpace(strings.ToLower((input))) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	case "":
		return defaultIfEmpty, nil
	}

	return false, errors.New("failed to parse confirmation input")
}

// confirmationValidator is intended to be passed to PromptWithValidation to
// determine if the confirmation input is valid.
func confirmationValidator(input string) error {
	_, err := parseConfirmationInput(input, false)
	return err
}

// Confirm outputs the message and prompts the user for a "yes" or "no"
// response.
func Confirm(message string, defaultIfEmpty bool) bool {
	input := PromptWithValidation(message, confirmationValidator)
	result, _ := parseConfirmationInput(input, defaultIfEmpty)
	return result
}

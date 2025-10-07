package cliutil_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/dansimau/yas/pkg/cliutil"
	"gotest.tools/v3/assert"
)

func TestPrompt_WithDefaultAndValidator_AcceptsEmptyInput(t *testing.T) {
	// Save original stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	assert.NilError(t, err)
	os.Stdin = r

	// Write empty input (just pressing enter)
	go func() {
		io.WriteString(w, "\n")
		w.Close()
	}()

	validator := func(input string) error {
		if input == "" {
			return errors.New("branch name cannot be empty")
		}
		return nil
	}

	result := cliutil.Prompt(cliutil.PromptOptions{
		Text:      "What is your trunk branch name?",
		Default:   "main",
		Validator: validator,
	})

	assert.Equal(t, result, "main")
}

func TestPrompt_WithDefaultAndValidator_AcceptsCustomInput(t *testing.T) {
	// Save original stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	assert.NilError(t, err)
	os.Stdin = r

	// Write custom input
	go func() {
		io.WriteString(w, "develop\n")
		w.Close()
	}()

	validator := func(input string) error {
		if input == "" {
			return errors.New("branch name cannot be empty")
		}
		return nil
	}

	result := cliutil.Prompt(cliutil.PromptOptions{
		Text:      "What is your trunk branch name?",
		Default:   "main",
		Validator: validator,
	})

	assert.Equal(t, result, "develop")
}

func TestPrompt_WithoutDefault_NoValidator(t *testing.T) {
	// Save original stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	assert.NilError(t, err)
	os.Stdin = r

	// Write empty input
	go func() {
		io.WriteString(w, "\n")
		w.Close()
	}()

	result := cliutil.Prompt(cliutil.PromptOptions{
		Text: "Enter something:",
	})

	assert.Equal(t, result, "")
}

func TestPrompt_WithDefaultNoValidator(t *testing.T) {
	// Save original stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	assert.NilError(t, err)
	os.Stdin = r

	// Write empty input (just pressing enter)
	go func() {
		io.WriteString(w, "\n")
		w.Close()
	}()

	result := cliutil.Prompt(cliutil.PromptOptions{
		Text:    "Enter branch name:",
		Default: "master",
	})

	assert.Equal(t, result, "master")
}


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
		_, err := io.WriteString(w, "\n")
		assert.NilError(t, err)
		assert.NilError(t, w.Close())
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
		_, err := io.WriteString(w, "develop\n")
		assert.NilError(t, err)
		assert.NilError(t, w.Close())
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
		_, err := io.WriteString(w, "\n")
		assert.NilError(t, err)
		assert.NilError(t, w.Close())
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
		_, err = io.WriteString(w, "\n")
		assert.NilError(t, err)
		assert.NilError(t, w.Close())
	}()

	result := cliutil.Prompt(cliutil.PromptOptions{
		Text:    "Enter branch name:",
		Default: "master",
	})

	assert.Equal(t, result, "master")
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase y", "y\n", true},
		{"uppercase Y", "Y\n", true},
		{"lowercase yes", "yes\n", true},
		{"uppercase YES", "YES\n", true},
		{"mixed case Yes", "Yes\n", true},
		{"lowercase n", "n\n", false},
		{"uppercase N", "N\n", false},
		{"lowercase no", "no\n", false},
		{"uppercase NO", "NO\n", false},
		{"empty input", "\n", false},
		{"whitespace around y", "  y  \n", true},
		{"whitespace around yes", "  yes  \n", true},
		{"random input", "maybe\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdin := os.Stdin

			defer func() { os.Stdin = oldStdin }()

			r, w, err := os.Pipe()
			assert.NilError(t, err)

			os.Stdin = r

			go func() {
				_, err := io.WriteString(w, tt.input)
				assert.NilError(t, err)
				assert.NilError(t, w.Close())
			}()

			result := cliutil.Confirm("Continue?")
			assert.Equal(t, result, tt.expected)
		})
	}
}

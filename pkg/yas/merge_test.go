package yas

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseMergeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		expectTitle string
		expectBody  string
	}{
		{
			name:        "title and body with blank line separator",
			input:       "Fix bug in merge logic\n\nThis fixes the issue where merges would fail.",
			expectTitle: "Fix bug in merge logic",
			expectBody:  "This fixes the issue where merges would fail.",
		},
		{
			name:        "title only",
			input:       "Fix bug in merge logic",
			expectTitle: "Fix bug in merge logic",
			expectBody:  "",
		},
		{
			name:        "title with trailing whitespace",
			input:       "Fix bug in merge logic   \n\nThis is the body.",
			expectTitle: "Fix bug in merge logic",
			expectBody:  "This is the body.",
		},
		{
			name:        "multiple blank lines after title",
			input:       "Title\n\n\n\nBody starts here",
			expectTitle: "Title",
			expectBody:  "Body starts here",
		},
		{
			name:        "body with multiple paragraphs",
			input:       "Title\n\nFirst paragraph.\n\nSecond paragraph.",
			expectTitle: "Title",
			expectBody:  "First paragraph.\n\nSecond paragraph.",
		},
		{
			name:        "empty input",
			input:       "",
			expectTitle: "",
			expectBody:  "",
		},
		{
			name:        "only blank lines",
			input:       "\n\n\n",
			expectTitle: "",
			expectBody:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a minimal YAS instance just for the method
			yas := &YAS{}

			title, body := yas.parseMergeMessage(tt.input)
			assert.Equal(t, title, tt.expectTitle, "title mismatch")
			assert.Equal(t, body, tt.expectBody, "body mismatch")
		})
	}
}

func TestRemoveStackSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "removes stack section with separator",
			input: `This is my PR description.

It has multiple paragraphs.

---

Stacked PRs:

* https://github.com/test/repo/pull/1
* https://github.com/test/repo/pull/2 👈 (this PR)`,
			expected: "This is my PR description.\n\nIt has multiple paragraphs.",
		},
		{
			name: "removes stack section without separator",
			input: `This is my PR description.

Stacked PRs:

* https://github.com/test/repo/pull/1`,
			expected: "This is my PR description.",
		},
		{
			name:     "no stack section - returns unchanged",
			input:    "This is my PR description.\n\nNo stacks here.",
			expected: "This is my PR description.\n\nNo stacks here.",
		},
		{
			name:     "empty body",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := removeStackSection(tt.input)
			assert.Equal(t, result, tt.expected)
		})
	}
}

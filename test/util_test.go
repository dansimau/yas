package test

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestLineBasedLeadingTrailingWhitespaceRegexp(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input    string
		expected string
	}{
		{
			input:    "  A  \n  B  \n  C\n",
			expected: "A\nB\nC",
		},
		{
			input:    "A\n\nB\nC",
			expected: "A\n\nB\nC",
		},
	} {
		assert.Equal(t, stripWhiteSpaceFromLines(test.input), test.expected)
	}
}

func TestEqualLines(t *testing.T) {
	t.Parallel()
	equalLines(t, `
		one
		  two
					three
		four

	`, `
		one
		two
		three
		four
	`)
}

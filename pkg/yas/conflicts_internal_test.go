package yas

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestUnexpectedChanges(t *testing.T) {
	t.Parallel()

	before := map[string]string{
		"conflicted.txt": "UU",
		"other.txt":      "UU",
		"dirty.txt":      " M",
		"scratch.txt":    "??",
		"gone.txt":       "??",
	}
	after := map[string]string{
		"conflicted.txt": "UU",
		"other.txt":      " M", // staged/changed conflicted file: ignored
		"dirty.txt":      " M", // unchanged
		"scratch.txt":    "A ", // status changed
		"helper.txt":     "??", // created by the resolver
		"code.go":        " M", // edited by the resolver
		// gone.txt disappeared
	}

	assert.DeepEqual(t, unexpectedChanges(before, after, []string{"conflicted.txt", "other.txt"}),
		[]string{"code.go", "gone.txt", "helper.txt", "scratch.txt"})

	assert.Equal(t, len(unexpectedChanges(before, before, nil)), 0)
}

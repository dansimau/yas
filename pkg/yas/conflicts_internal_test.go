package yas

import (
	"testing"

	"github.com/dansimau/yas/pkg/gitexec"
	"gotest.tools/v3/assert"
)

func TestUnexpectedChanges(t *testing.T) {
	t.Parallel()

	entry := func(status string, size int64) gitexec.StatusEntry {
		return gitexec.StatusEntry{Status: status, Mode: 0o644, Size: size, ModTime: 1}
	}

	before := map[string]gitexec.StatusEntry{
		"conflicted.txt": entry("UU", 10),
		"other.txt":      entry("UU", 10),
		"dirty.txt":      entry(" M", 10),
		"scratch.txt":    entry("??", 10),
		"notes.txt":      entry("??", 10),
		"secret.env":     entry("!!", 10),
		"gone.txt":       entry("??", 10),
	}
	after := map[string]gitexec.StatusEntry{
		"conflicted.txt": entry("UU", 10),
		"other.txt":      entry(" M", 12), // staged/changed conflicted file: ignored
		"dirty.txt":      entry(" M", 10), // unchanged
		"scratch.txt":    entry("A ", 10), // status changed
		"notes.txt":      entry("??", 14), // still untracked, but overwritten
		"secret.env":     entry("!!", 10), // ignored file, untouched
		"helper.txt":     entry("??", 7),  // created by the resolver
		"code.go":        entry(" M", 99), // edited by the resolver
		// gone.txt disappeared
	}

	assert.DeepEqual(t, unexpectedChanges(before, after, []string{"conflicted.txt", "other.txt"}),
		[]string{"code.go", "gone.txt", "helper.txt", "notes.txt", "scratch.txt"})

	// An ignored file rewritten with the same size is still caught via mtime.
	after["secret.env"] = gitexec.StatusEntry{Status: "!!", Mode: 0o644, Size: 10, ModTime: 2}
	assert.DeepEqual(t, unexpectedChanges(before, after, []string{"conflicted.txt", "other.txt"}),
		[]string{"code.go", "gone.txt", "helper.txt", "notes.txt", "scratch.txt", "secret.env"})

	assert.Equal(t, len(unexpectedChanges(before, before, nil)), 0)
}

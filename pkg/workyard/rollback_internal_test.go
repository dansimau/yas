package workyard

import (
	"errors"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRollbackRunsInReverseAndReportsAllErrors(t *testing.T) {
	t.Parallel()

	var order []string

	r := &rollback{}
	r.add("first", func() error {
		order = append(order, "first")

		return nil
	})
	r.add("second", func() error {
		order = append(order, "second")

		return errors.New("boom")
	})
	r.add("third", func() error {
		order = append(order, "third")

		return errors.New("bang")
	})

	err := r.run()

	assert.DeepEqual(t, order, []string{"third", "second", "first"})
	assert.Error(t, err, "Errors while rolling back:\n* third: bang\n* second: boom")

	// Steps run once.
	assert.NilError(t, r.run())
	assert.Equal(t, len(order), 3)
}

func TestRollbackWithoutErrors(t *testing.T) {
	t.Parallel()

	r := &rollback{}
	r.add("only", func() error { return nil })
	assert.NilError(t, r.run())
}

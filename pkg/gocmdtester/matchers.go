package gocmdtester

// ArgMatcher is a special argument matcher for use with Mock().
// These constants can be used in place of string arguments when creating mocks
// to match patterns instead of exact values.
type ArgMatcher string

const (
	// Any matches exactly one argument with any value.
	// Example: Mock("git", "merge-base", Any) matches "git merge-base foo"
	// but NOT "git merge-base" or "git merge-base foo bar".
	Any ArgMatcher = "__MOCKSHIM_ANY__"

	// AnyFurtherArgs matches zero or more remaining arguments.
	// Must be the last argument in the pattern.
	// Example: Mock("git", "push", AnyFurtherArgs) matches "git push",
	// "git push origin", and "git push origin main --force".
	AnyFurtherArgs ArgMatcher = "__MOCKSHIM_ANY_FURTHER__"
)

// String returns the string representation of the matcher.
func (m ArgMatcher) String() string {
	return string(m)
}

// argsMatch checks if actual arguments match a pattern.
// The pattern can contain special matcher strings (Any, AnyFurtherArgs).
func argsMatch(pattern, actual []string) bool {
	patternIdx := 0
	actualIdx := 0

	for patternIdx < len(pattern) {
		if pattern[patternIdx] == string(AnyFurtherArgs) {
			// AnyFurtherArgs must be the last element in pattern
			// It matches zero or more remaining arguments
			return patternIdx == len(pattern)-1
		}

		if actualIdx >= len(actual) {
			return false // More pattern elements than actual args
		}

		if pattern[patternIdx] == string(Any) {
			// Any matches exactly one argument
			patternIdx++
			actualIdx++

			continue
		}

		// Exact match required
		if pattern[patternIdx] != actual[actualIdx] {
			return false
		}

		patternIdx++
		actualIdx++
	}

	// All pattern elements consumed; actual args must also be consumed
	return actualIdx == len(actual)
}

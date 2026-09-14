package workyardcli

// Exit codes.
const (
	ExitOK = 0
	// ExitFailure is a command that ran but failed (including a repository's
	// git command exiting non-zero).
	ExitFailure = 1
	// ExitUsage is a usage error or not being inside a workyard.
	ExitUsage = 2
)

// Error is an error thrown by the CLI: it is printed as "ERROR: <message>"
// (without a stack trace) and sets the exit code.
type Error struct {
	msg      string
	err      error
	exitCode int
}

// NewError returns an Error with exit code 1.
func NewError(msg string) *Error {
	return &Error{msg: msg, exitCode: ExitFailure}
}

// NewUsageError returns an Error with exit code 2.
func NewUsageError(msg string) *Error {
	return &Error{msg: msg, exitCode: ExitUsage}
}

// WrapError wraps err in an Error with the given exit code.
func WrapError(err error, exitCode int) *Error {
	return &Error{msg: err.Error(), err: err, exitCode: exitCode}
}

func (e *Error) Error() string {
	return e.msg
}

func (e *Error) Unwrap() error {
	return e.err
}

func (e *Error) ExitCode() int {
	return e.exitCode
}

package yascli

// Error is an error thrown by the CLI and it causes the CLI to exit with a
// message, e.g. "ERROR: Aborted." or similar. If the CLI exits with an error
// that is not Error, it will attempt to print a stack trace.
type Error struct {
	msg string
}

func NewError(msg string) *Error {
	return &Error{msg: msg}
}

func (e *Error) Error() string {
	return e.msg
}

package xexec

import "io"

// These options are designed to be chained for easy reading/writing. It also
// allows Cmd constructors to take variadic args for the command arguments,
// which further reduces boilerplate.

func (c *Cmd) WithStdout(w io.Writer) *Cmd {
	c.Stdout = w
	return c
}

func (c *Cmd) WithStderr(w io.Writer) *Cmd {
	c.Stderr = w
	return c
}

func (c *Cmd) WithStdin(r io.Reader) *Cmd {
	c.Stdin = r
	return c
}

func (c *Cmd) WithWorkingDir(dir string) *Cmd {
	c.Dir = dir
	return c
}

func (c *Cmd) WithEnvVars(vars []string) *Cmd {
	c.Env = vars
	return c
}

// Verbose enables printing of the command to stderr before it is run.
func (c *Cmd) Verbose(enabled bool) *Cmd {
	c.verbose = enabled
	return c
}

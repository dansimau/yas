package gocmdtester

import "io"

// runConfig holds the configuration for Run invocations.
type runConfig struct {
	env        map[string]string
	workingDir string
	stdin      io.Reader
}

// Option is a functional option for configuring a CmdTester.
type Option func(*runConfig)

// WithEnv sets an environment variable for command execution.
// Multiple calls to WithEnv with the same key will use the last value.
//
// Example:
//
//	tester, err := gocmdtester.FromPath("./cmd/myapp",
//	    gocmdtester.WithEnv("DEBUG", "true"),
//	    gocmdtester.WithEnv("LOG_LEVEL", "verbose"))
func WithEnv(key, value string) Option {
	return func(c *runConfig) {
		if c.env == nil {
			c.env = make(map[string]string)
		}

		c.env[key] = value
	}
}

// WithWorkingDir sets the working directory for command execution.
//
// Example:
//
//	tester, err := gocmdtester.FromPath("./cmd/myapp",
//	    gocmdtester.WithWorkingDir("/tmp/test-repo"))
func WithWorkingDir(path string) Option {
	return func(c *runConfig) {
		c.workingDir = path
	}
}

// WithStdin sets the stdin reader for command execution.
//
// Example:
//
//	input := strings.NewReader("yes\nconfirm\n")
//	tester, err := gocmdtester.FromPath("./cmd/myapp",
//	    gocmdtester.WithStdin(input))
func WithStdin(r io.Reader) Option {
	return func(c *runConfig) {
		c.stdin = r
	}
}

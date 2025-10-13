// Package xexec provides a wrapper for exec.Cmd that allows for easy
// debugging and verbose output.
package xexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
	"gopkg.in/alessio/shellescape.v1"
)

// Cmd is a wrapper for exec.Cmd.
type Cmd struct {
	*exec.Cmd

	// when verbose is true, commands will be printed to os.Stderr before they
	// are executed.
	verbose bool
}

// cmdConstructor is the internal constructor for a Cmd; it is shared by two
// exported constructor functions below.
func cmdConstructor(c *Cmd) *Cmd {
	// Run commands as "interactive" by default. This can be overridden by
	// using options such as WithStderr(nil).
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// Allow easy debugging for operators by setting an environment variable
	// which will cause xexec to print any command before it executes it
	// (similar to bash -x).
	if os.Getenv("XEXEC_VERBOSE") != "" {
		c.verbose = true
	}

	return c
}

// Command creates a new wrapped exec.Cmd.
func Command(args ...string) *Cmd {
	return cmdConstructor(&Cmd{
		Cmd: exec.Command(args[0], args[1:]...),
	})
}

// CommandContext creates a new wrapped exec.Cmd with context.
func CommandContext(ctx context.Context, args ...string) *Cmd {
	return cmdConstructor(&Cmd{
		Cmd: exec.CommandContext(ctx, args[0], args[1:]...),
	})
}

// debugPrintCmd prints the command args to stderr.
func (c *Cmd) debugPrintCmd() {
	// Quote args before printing where necessary; this makes it easy for a
	// human to copy the line and paste it in their tty if they want to run
	// something manually
	quotedArgs := []string{}
	for _, arg := range c.Args {
		quotedArgs = append(quotedArgs, shellescape.Quote(arg))
	}

	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintf(os.Stderr, "\033[1;30m+ %s\033[0m\n", strings.Join(quotedArgs, " "))
	} else {
		fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(quotedArgs, " "))
	}
}

// Run is like exec.Run that always captures stderr output into the returned
// error (exec.ExitError{}.Stderr).
func (c *Cmd) Run() error {
	if c.verbose {
		c.debugPrintCmd()
	}

	var (
		w      io.Writer
		stderr bytes.Buffer
	)

	// If a c.Stderr has already been provided, create a multiwriter to write
	// to both the existing c.Stderr as well as our own buffer (for storing on
	// the error).

	if c.Stderr != nil {
		w = io.MultiWriter(&stderr, c.Stderr)
	} else {
		w = &stderr
	}

	c.Stderr = w

	if err := c.Cmd.Run(); err != nil {
		// Store stderr onto the exec error itself so users can access this
		// if needed.
		ee := &exec.ExitError{}
		if errors.As(err, &ee) {
			ee.Stderr = stderr.Bytes()
		}

		return err
	}

	return nil
}

// Output is like exec.Output, except that xexec captures the output in
// addition to writing output to any existing provided c.Stdout.
func (c *Cmd) Output() ([]byte, error) {
	var stdout bytes.Buffer

	c.Stdout = createMultiWriter(&stdout, c.Stdout)

	err := c.Run()

	return stdout.Bytes(), err
}

// CombinedOutput is like exec.CombinedOutput, except that xexec captures the
// output in addition to writing output to any existing provided
// c.Stdout/c.Stderr.
func (c *Cmd) CombinedOutput() ([]byte, error) {
	if c.verbose {
		c.debugPrintCmd()
	}

	var buf bytes.Buffer

	c.Stdout = createMultiWriter(&buf, c.Stdout)
	c.Stderr = createMultiWriter(&buf, c.Stderr)

	err := c.Run()

	return buf.Bytes(), err
}

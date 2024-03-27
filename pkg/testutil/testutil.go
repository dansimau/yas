package testutil

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/dansimau/yas/pkg/xexec"
)

// CaptureOutput temporarily overrides os.Stdout and os.Stderr, runs the
// specified function and returns any output written. Note: footgun alert: if
// the fn calls os.Exit this will screw up stdout/stderr during tests.
func CaptureOutput(fn func()) (stdout string, stderr string, err error) {
	prevStdout := os.Stdout
	prevStderr := os.Stderr

	defer func() {
		os.Stdout = prevStdout
		os.Stderr = prevStderr
	}()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return "", "", err
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	errC := make(chan error, 2)

	stdoutC := make(chan string)
	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(io.MultiWriter(&buf, prevStdout), stdoutR); err != nil {
			errC <- err
			return
		}
		stdoutC <- buf.String()
	}()

	stderrC := make(chan string)
	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(io.MultiWriter(&buf, prevStderr), stderrR); err != nil {
			errC <- err
		}
		stderrC <- buf.String()
	}()

	fn()

	os.Stdout = prevStdout
	os.Stderr = prevStderr

	stdoutW.Close()
	stderrW.Close()

	stdout = <-stdoutC
	stderr = <-stderrC

	stdoutR.Close()
	stderrR.Close()

	close(stdoutC)
	close(stderrC)
	defer close(errC)

	select {
	case err := <-errC:
		return stdout, stderr, err
	default:
	}

	return stdout, stderr, nil
}

func ExecOrFail(t *testing.T, lines string) {
	f, err := os.CreateTemp("", "exec*.sh")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		f.Close()

		if !t.Failed() {
			if err := os.Remove(f.Name()); err != nil {
				log.Println("failed to remove temp file", f.Name())
			}
		} else {
			log.Println("test failed, not cleaning up temp file", f.Name())
		}
	}()

	_, err = f.WriteString(lines)
	if err != nil {
		t.Fatal(err)
	}

	f.Close()

	if err := xexec.Command("chmod", "+x", f.Name()).Run(); err != nil {
		t.Fatal(err)
	}

	if err := xexec.Command("sh", "-c", f.Name()).Run(); err != nil {
		t.Fatal(err)
	}
}

// WithEnv clears the current environment and sets it to the provided vars. It
// returns a function to restore the env to it's previous values.
func WithEnv(vars ...string) func() {
	prevEnv := os.Environ()
	os.Clearenv()

	for _, v := range vars {
		os.Setenv(parseEnvVar(v))
	}

	return func() {
		os.Clearenv()
		for _, v := range prevEnv {
			os.Setenv(parseEnvVar(v))
		}
	}
}

// WithTempWorkingDir creates a temporary directory and switches to that
// directory for the duration of the test. When the test is complete, it
// switches back to the original working directory.
//
// If the test passes, the directory is cleaned up. If the test fails, the temp
// directory is left in place and the path to it is printed.
func WithTempWorkingDir(t *testing.T, fn func()) {
	tempDirPath, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if !t.Failed() {
			if err := os.RemoveAll(tempDirPath); err != nil {
				log.Println("failed to remove temporary directory", tempDirPath)
			}
		} else {
			log.Println("test failed, not cleaning up temporary directory", tempDirPath)
		}
	}()

	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(tempDirPath); err != nil {
		t.Fatal(err)
	}

	defer os.Chdir(prevDir)

	fn()
}

// parseEnvVar parses an env var string e.g. "foo=bar" and returns the
// key/value components. It panics if the input string is invalid.
func parseEnvVar(s string) (key, value string) {
	// Empty strings are possible in the go stdlib to represent deleted or
	// duplicate values.
	if s == "" {
		return "", ""
	}

	v := strings.SplitN(s, "=", 2)

	// Handle the case where we have an invalid env var (i.e. with "=")
	if len(v) < 2 {
		panic(fmt.Sprintf("invalid env var: %s", s))
	}

	return v[0], v[1]
}

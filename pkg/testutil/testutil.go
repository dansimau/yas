// Package testutil provides utilities for testing.
package testutil

import (
	"bytes"
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

	if err := stdoutW.Close(); err != nil {
		return "", "", err
	}

	if err := stderrW.Close(); err != nil {
		return "", "", err
	}

	stdout = <-stdoutC
	stderr = <-stderrC

	if err := stdoutR.Close(); err != nil {
		return "", "", err
	}

	if err := stderrR.Close(); err != nil {
		return "", "", err
	}

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
	f, err := os.CreateTemp(t.TempDir(), "exec*.sh")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		// ignore error as file may already be closed from below
		_ = f.Close()

		if !t.Failed() {
			if err := os.Remove(f.Name()); err != nil {
				t.Fatal("failed to remove temp file", f.Name(), err)
			}
		} else {
			t.Fatal("test failed, not cleaning up temp file", f.Name())
		}
	}()

	_, err = f.WriteString(lines)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

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
		if err := os.Setenv(parseEnvVar(v)); err != nil {
			panic(err)
		}
	}

	return func() {
		os.Clearenv()

		for _, v := range prevEnv {
			if err := os.Setenv(parseEnvVar(v)); err != nil {
				panic(err)
			}
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
	t.Helper()

	tempDirPath, err := os.MkdirTemp("", "yas-test-")
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

	t.Chdir(tempDirPath)

	defer func() {
		t.Chdir(prevDir)
	}()

	fn()
}

// SetupFakeRemote configures a fake origin remote for testing purposes.
// It sets up the remote and configures the specified branch to track it.
// The remote URL is set to a fake GitHub URL.
func SetupFakeRemote(t *testing.T, branchName string) {
	t.Helper()

	// Configure the remote
	if err := xexec.Command("git", "config", "remote.origin.url", "https://github.com/test/test.git").Run(); err != nil {
		t.Fatal(err)
	}

	if err := xexec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*").Run(); err != nil {
		t.Fatal(err)
	}

	// Configure the branch to track the remote
	if err := xexec.Command("git", "config", "branch."+branchName+".remote", "origin").Run(); err != nil {
		t.Fatal(err)
	}

	if err := xexec.Command("git", "config", "branch."+branchName+".merge", "refs/heads/"+branchName).Run(); err != nil {
		t.Fatal(err)
	}
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
		panic("invalid env var: " + s)
	}

	return v[0], v[1]
}

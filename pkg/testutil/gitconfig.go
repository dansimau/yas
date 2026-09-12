package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dansimau/yas/pkg/xexec"
)

// preserveGoEnvVars are Go environment variables whose defaults are derived
// from HOME. They are pinned to their current effective values before
// HOME is changed so that `go build` (run by gocmdtester from inside the test
// process) keeps using the real caches and settings.
var preserveGoEnvVars = []string{"GOENV", "GOPATH", "GOCACHE", "GOMODCACHE"}

// IsolateGitConfig points HOME (and XDG_CONFIG_HOME) at a temporary directory
// containing a minimal global git config, so tests behave the same regardless
// of the developer's own ~/.gitconfig (e.g. commit.gpgsign, includes, aliases).
// Call it from TestMain in any package whose tests run git. The returned
// function restores the environment and removes the temporary directory.
//
// Not thread safe: it modifies the process environment, so it must not be
// called while other goroutines (e.g. parallel tests) may be reading it.
func IsolateGitConfig() func() {
	pinGoEnv()

	home, err := os.MkdirTemp("", "yas-test-home-")
	if err != nil {
		panic(err)
	}

	gitconfig := `[user]
	name = Test User
	email = test@example.com
[commit]
	gpgsign = false
`

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		panic(err)
	}

	restore := setEnv(map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
	})

	return func() {
		restore()

		_ = os.RemoveAll(home)
	}
}

// pinGoEnv sets each Go environment variable in preserveGoEnvVars to
// its current effective value as reported by `go env`.
//
// Not thread safe: it modifies the process environment.
func pinGoEnv() {
	out, err := xexec.Command(append([]string{"go", "env", "-json"}, preserveGoEnvVars...)...).Output()
	if err != nil {
		panic(err)
	}

	values := map[string]string{}
	if err := json.Unmarshal(out, &values); err != nil {
		panic(err)
	}

	for name, value := range values {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
}

// setEnv sets the given environment variables and returns a function that
// restores their previous values.
//
// Not thread safe: both it and the returned function modify the process
// environment.
func setEnv(vars map[string]string) func() {
	type prev struct {
		value string
		set   bool
	}

	prevs := map[string]prev{}

	for name, value := range vars {
		v, ok := os.LookupEnv(name)
		prevs[name] = prev{value: v, set: ok}

		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}

	return func() {
		for name, p := range prevs {
			var err error
			if p.set {
				err = os.Setenv(name, p.value)
			} else {
				err = os.Unsetenv(name)
			}

			if err != nil {
				panic(err)
			}
		}
	}
}

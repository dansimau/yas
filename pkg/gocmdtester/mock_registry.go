package gocmdtester

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// mockRegistry manages mocks for a single CmdTester.
// Each tester has its own registry with a unique temporary directory.
type mockRegistry struct {
	mockDir  string  // Temp dir containing config, invocations, and shims subdir
	shimsDir string  // Subdir with symlinks to mockshim binary
	mocks    []*Mock // Registered mocks
	mu       sync.Mutex
}

// newMockRegistry creates a new mock registry with a temporary directory.
func newMockRegistry() (*mockRegistry, error) {
	mockDir, err := os.MkdirTemp("", "gocmdtester-mocks-")
	if err != nil {
		return nil, fmt.Errorf("failed to create mock directory: %w", err)
	}

	shimsDir := filepath.Join(mockDir, "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		_ = os.RemoveAll(mockDir)

		return nil, fmt.Errorf("failed to create shims directory: %w", err)
	}

	return &mockRegistry{
		mockDir:  mockDir,
		shimsDir: shimsDir,
	}, nil
}

// addMock adds a mock to the registry.
// If this is the first mock for the given command, a symlink is created.
func (r *mockRegistry) addMock(m *Mock) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if we already have a shim for this command
	shimPath := filepath.Join(r.shimsDir, m.command)
	if _, err := os.Stat(shimPath); os.IsNotExist(err) {
		// Need to create symlink to mockshim binary
		shimBinary, err := ensureShimCompiled()
		if err != nil {
			return fmt.Errorf("failed to compile mockshim: %w", err)
		}

		if err := os.Symlink(shimBinary, shimPath); err != nil {
			return fmt.Errorf("failed to create shim symlink: %w", err)
		}
	}

	m.registry = r
	r.mocks = append(r.mocks, m)

	return nil
}

// writeConfig writes the mock configuration file.
// This should be called before each Run() to ensure the config is up to date.
func (r *mockRegistry) writeConfig() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	configs := make([]mockConfig, len(r.mocks))
	for i, m := range r.mocks {
		configs[i] = m.toConfig()
	}

	data, err := json.Marshal(configs)
	if err != nil {
		return fmt.Errorf("failed to marshal mock config: %w", err)
	}

	configPath := filepath.Join(r.mockDir, "mock_config.json")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write mock config: %w", err)
	}

	return nil
}

// loadInvocations loads all invocation logs from the mock directory.
// Multiple files may exist due to parallel execution (one per PID).
func (r *mockRegistry) loadInvocations() ([]Invocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pattern := filepath.Join(r.mockDir, "mock_invocations_*.ndjson")

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob invocation files: %w", err)
	}

	var invocations []Invocation

	for _, file := range files {
		fileInvocations, err := loadInvocationsFromFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load invocations from %s: %w", file, err)
		}

		invocations = append(invocations, fileInvocations...)
	}

	return invocations, nil
}

// loadInvocationsFromFile loads invocations from a single NDJSON file.
func loadInvocationsFromFile(path string) ([]Invocation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var invocations []Invocation

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var inv Invocation
		if err := json.Unmarshal([]byte(line), &inv); err != nil {
			return nil, fmt.Errorf("failed to parse invocation: %w", err)
		}

		invocations = append(invocations, inv)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read invocations: %w", err)
	}

	return invocations, nil
}

// verifyAllCalled checks that all registered mocks were called at least once.
// Returns an error listing any uncalled mocks.
// Pattern-based mocks (using Any or AnyFurtherArgs) are verified using pattern matching.
func (r *mockRegistry) verifyAllCalled() error {
	invocations, err := r.loadInvocations()
	if err != nil {
		return fmt.Errorf("failed to load invocations: %w", err)
	}

	var uncalled []string

	r.mu.Lock()
	mocks := r.mocks
	r.mu.Unlock()

	for _, mock := range mocks {
		found := false

		for _, inv := range invocations {
			if inv.Command == mock.command && argsMatch(mock.args, inv.Args) {
				found = true

				break
			}
		}

		if !found {
			if len(mock.args) > 0 {
				uncalled = append(uncalled, fmt.Sprintf("%s %v", mock.command, mock.args))
			} else {
				uncalled = append(uncalled, mock.command)
			}
		}
	}

	if len(uncalled) > 0 {
		return fmt.Errorf("mocks not called: %s", strings.Join(uncalled, ", "))
	}

	return nil
}

// mockDir returns the path to the mock directory.
func (r *mockRegistry) getMockDir() string {
	return r.mockDir
}

// shimsDir returns the path to the shims directory.
func (r *mockRegistry) getShimsDir() string {
	return r.shimsDir
}

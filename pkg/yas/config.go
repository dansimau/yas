package yas

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
	"github.com/dansimau/yas/pkg/gitexec"
	"gopkg.in/yaml.v2"
)

var (
	configFiles = []string{
		".yas/yas.yaml",
		".git/yas.yaml", // Deprecated
	}

	stateFiles = []string{
		".yas/yas.state.json",
		".git/.yasstate", // Deprecated
	}

	defaultWorktreesPath = ".yas/worktrees"
)

type Config struct {
	RepoDirectory    string `yaml:"-"`
	TrunkBranch      string `yaml:"trunkBranch"`
	AutoPrefixBranch bool   `yaml:"autoPrefixBranch"`
	WorktreeBranch   bool   `yaml:"worktreeBranch"`
	WorktreesPath    string `yaml:"worktreesPath"`

	// TrunkBranchAliases is a list of branch names that, when given as an
	// argument, are treated as referring to TrunkBranch. This helps when
	// switching between repos whose trunk branches have different names (e.g.
	// aliasing "master" to "main").
	TrunkBranchAliases []string `yaml:"trunkBranchAliases,omitempty"`
}

// getYASConfigBase returns the base path for the YAS config files. This is the
// primary worktree path.
func getYASConfigBase(repoDir string) (string, error) {
	return gitexec.WithRepo(repoDir).PrimaryWorktreePath()
}

// resolveFirstFilePath returns the first file path that exists, or the first
// path if none exist.
func resolveFirstFilePath(repoDir string, candidates []string) (string, error) {
	configBasePath, err := getYASConfigBase(repoDir)
	if err != nil {
		return "", err
	}

	for _, filename := range candidates {
		fullPath := path.Join(configBasePath, filename)

		exists, err := fsutil.FileExists(fullPath)
		if err != nil {
			return "", err
		}

		if exists {
			return fullPath, nil
		}
	}

	// No file exists - use first (new) path for writing
	return path.Join(configBasePath, candidates[0]), nil
}

// resolveConfigPath returns the first config path that exists, or the first
// path if none exist (for writing to the new location).
func resolveConfigPath(repoDir string) (string, error) {
	return resolveFirstFilePath(repoDir, configFiles)
}

// resolveStatePath returns the first state path that exists, or the first
// path if none exist (for writing to the new location).
func resolveStatePath(repoDir string) (string, error) {
	return resolveFirstFilePath(repoDir, stateFiles)
}

// errNotConfigured is returned when the repository has no usable yas config.
var errNotConfigured = errors.New("repository not configured (hint: run `yas init`)")

// validateRequired checks that all required config values are set. A config
// file that exists but is missing required values is treated as unconfigured.
func (cfg Config) validateRequired() error {
	if cfg.TrunkBranch == "" {
		return errors.New("trunk branch is not set")
	}

	return nil
}

// ConfigFileExists reports whether a yas config file exists for the
// repository, regardless of whether it is complete.
func ConfigFileExists(repoDirectory string) (bool, error) {
	configPath, err := resolveConfigPath(repoDirectory)
	if err != nil {
		return false, err
	}

	return fsutil.FileExists(configPath)
}

// LoadConfig reads the config file for the repository if one exists. Unlike
// ReadConfig, it does not require the config to be complete, so it is suitable
// for commands that are about to (re)write the config.
//
// If a config file exists, defaults are applied for keys it does not set (for
// backward compatibility with older config files). If no config file exists,
// a zero-value config is returned and callers decide which defaults apply.
func LoadConfig(repoDirectory string) (*Config, error) {
	exists, err := ConfigFileExists(repoDirectory)
	if err != nil {
		return nil, err
	}

	if !exists {
		return &Config{RepoDirectory: repoDirectory}, nil
	}

	configPath, err := resolveConfigPath(repoDirectory)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// Default AutoPrefixBranch to true for backward compatibility
	config := Config{
		AutoPrefixBranch: true,
		WorktreesPath:    defaultWorktreesPath,
	}
	if err := yaml.Unmarshal(yamlBytes, &config); err != nil {
		return nil, err
	}

	config.RepoDirectory = repoDirectory

	return &config, nil
}

// IsConfigured reports whether the repository has a config file with all
// required values set (currently: the trunk branch).
func IsConfigured(repoDirectory string) (bool, error) {
	exists, err := ConfigFileExists(repoDirectory)
	if err != nil || !exists {
		return false, err
	}

	cfg, err := LoadConfig(repoDirectory)
	if err != nil {
		return false, err
	}

	return cfg.validateRequired() == nil, nil
}

// ReadConfig reads the repository config and returns an error if the
// repository is not configured (no config file, or required values missing).
func ReadConfig(repoDirectory string) (*Config, error) {
	isConfigured, err := IsConfigured(repoDirectory)
	if err != nil {
		return nil, err
	}

	if !isConfigured {
		return nil, errNotConfigured
	}

	return LoadConfig(repoDirectory)
}

// WriteConfig writes config to config file underneath the repo directory
// (defined) in the config itself. It returns the path to the file it wrote to.
func WriteConfig(cfg Config) (string, error) {
	if err := cfg.validateRequired(); err != nil {
		return "", fmt.Errorf("cannot write config: %w (hint: run `yas init` or pass --trunk-branch)", err)
	}

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	configFilePath, err := resolveConfigPath(cfg.RepoDirectory)
	if err != nil {
		return "", err
	}

	// Ensure the directory exists
	if err := os.MkdirAll(path.Dir(configFilePath), 0o755); err != nil {
		return "", err
	}

	if err := os.WriteFile(configFilePath, yamlBytes, 0o644); err != nil {
		return "", err
	}

	return configFilePath, nil
}

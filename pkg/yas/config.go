package yas

import (
	"errors"
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

func IsConfigured(repoDirectory string) (bool, error) {
	configPath, err := resolveConfigPath(repoDirectory)
	if err != nil {
		return false, err
	}

	return fsutil.FileExists(configPath)
}

func ReadConfig(repoDirectory string) (*Config, error) {
	isConfigured, err := IsConfigured(repoDirectory)
	if err != nil {
		return nil, err
	}

	if !isConfigured {
		return nil, errors.New("repository not configured (hint: run `yas init`)")
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

// WriteConfig writes config to config file underneath the repo directory
// (defined) in the config itself. It returns the path to the file it wrote to.
func WriteConfig(cfg Config) (string, error) {
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

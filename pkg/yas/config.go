package yas

import (
	"errors"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
	"gopkg.in/yaml.v2"
)

var configFilenames = []string{".yas/yas.yaml", ".git/yas.yaml"}

type Config struct {
	RepoDirectory    string `yaml:"-"`
	TrunkBranch      string `yaml:"trunkBranch"`
	AutoPrefixBranch bool   `yaml:"autoPrefixBranch"`
}

// resolveConfigPath returns the first config path that exists, or the first
// path if none exist (for writing to the new location).
func resolveConfigPath(repoDir string) (string, error) {
	for _, filename := range configFilenames {
		fullPath := path.Join(repoDir, filename)

		exists, err := fsutil.FileExists(fullPath)
		if err != nil {
			return "", err
		}

		if exists {
			return fullPath, nil
		}
	}
	// No file exists - use first (new) path for writing
	return path.Join(repoDir, configFilenames[0]), nil
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

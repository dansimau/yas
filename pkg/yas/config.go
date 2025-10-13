package yas

import (
	"errors"
	"os"
	"path"

	"github.com/dansimau/yas/pkg/fsutil"
	"gopkg.in/yaml.v2"
)

const configFilename = ".git/yas.yaml"

type Config struct {
	RepoDirectory    string `yaml:"-"`
	TrunkBranch      string `yaml:"trunkBranch"`
	AutoPrefixBranch bool   `yaml:"autoPrefixBranch"`
}

func IsConfigured(repoDirectory string) bool {
	return fsutil.FileExists(path.Join(repoDirectory, configFilename))
}

func ReadConfig(repoDirectory string) (*Config, error) {
	if !IsConfigured(repoDirectory) {
		return nil, errors.New("repository not configured (hint: run `yas init`)")
	}

	yamlBytes, err := os.ReadFile(path.Join(repoDirectory, configFilename))
	if err != nil {
		return nil, err
	}

	config := Config{}
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

	configFilePath := path.Join(cfg.RepoDirectory, configFilename)
	if err := os.WriteFile(configFilePath, yamlBytes, 0o644); err != nil {
		return "", err
	}

	return configFilePath, nil
}

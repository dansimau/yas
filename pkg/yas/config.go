package yas

import (
	"os"
	"path"

	"gopkg.in/yaml.v2"
)

const configFilename = "yas.yaml"

type Config struct {
	RepoDirectory string `yaml:"-"`
	TrunkBranch   string `yaml:"trunkBranch"`
}

func readConfig(repoDirectory string) (*Config, error) {
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

// writeConfig writes config to config file underneath the repo directory
// (defined) in the config itself. It returns the path to the file it wrote to.
func writeConfig(cfg Config) (string, error) {
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}

	configFilePath := path.Join(cfg.RepoDirectory, configFilename)
	if err := os.WriteFile(configFilePath, yamlBytes, 0644); err != nil {
		return "", err
	}

	return configFilePath, nil
}

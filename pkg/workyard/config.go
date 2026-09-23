package workyard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// LoadConfig reads <source>/.workyard/config.yaml. A missing file yields the
// zero Config; unknown keys are an error.
func LoadConfig(source string) (Config, error) {
	var cfg Config

	b, err := os.ReadFile(filepath.Join(source, workyardDir, configFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}

		return cfg, err
	}

	if err := yaml.UnmarshalStrict(b, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid %s/%s: %w", workyardDir, configFile, err)
	}

	if cfg.Version > MetadataVersion {
		return cfg, fmt.Errorf("%s/%s: unsupported version %d", workyardDir, configFile, cfg.Version)
	}

	return cfg, nil
}

// trunkFor returns the configured trunk for a repository path, falling back
// to the global trunk, or "" when neither is set.
func (c Config) trunkFor(repoPath string) string {
	if rc, ok := c.Repos[repoPath]; ok && rc.Trunk != "" {
		return rc.Trunk
	}

	return c.Trunk
}

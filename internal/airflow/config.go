package airflow

import (
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// ProjectConfig is the .skaledata.yaml file in the project root.
type ProjectConfig struct {
	Cluster string `yaml:"cluster"`
	App     string `yaml:"app,omitempty"`
}

// LoadConfig reads .skaledata.yaml from the project directory.
func LoadConfig(dir string) *ProjectConfig {
	data, err := os.ReadFile(filepath.Join(dir, ".skaledata.yaml"))
	if err != nil {
		return nil
	}
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// SaveConfig writes .skaledata.yaml to the project directory.
func SaveConfig(dir string, cfg *ProjectConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".skaledata.yaml"), data, 0o644)
}

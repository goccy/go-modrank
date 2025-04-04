package modrank

import (
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Database     string   `yaml:"database"`
	Organization string   `yaml:"organization"`
	Repositories []string `yaml:"repositories"`
	ClonePath    string   `yaml:"clonePath"`
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.UnmarshalWithOptions(f, &cfg, yaml.Strict()); err != nil {
		return nil, err
	}
	return &cfg, nil
}

package config

import (
	"gopkg.in/yaml.v3"
)

func Parse(data []byte) (*CodeBoltConfig, error) {
	var cfg CodeBoltConfig
	err := yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

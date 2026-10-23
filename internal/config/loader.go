package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const FileName = ".mcp-pipeline.json"

// LoadConfig reads configPath when it is set. An empty configPath reads
// projectRoot/.mcp-pipeline.json and returns DefaultConfig when that file is absent.
// A missing explicit path is ERR_CONFIG_NOT_FOUND. Malformed JSON and Validate
// failures are ERR_CONFIG_INVALID.
func LoadConfig(projectRoot, configPath string) (*PipelineConfig, error) {
	explicit := strings.TrimSpace(configPath) != ""
	path := configPath
	if !explicit {
		path = filepath.Join(projectRoot, FileName)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if explicit {
				return nil, &Error{Code: ErrNotFound, Detail: path}
			}
			return DefaultConfig(), nil
		}
		return nil, &Error{Code: ErrInvalid, Detail: err.Error()}
	}
	var cfg PipelineConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, &Error{Code: ErrInvalid, Detail: err.Error()}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

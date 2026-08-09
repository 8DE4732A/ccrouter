package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Error is a configuration validation error.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, a ...any) error {
	return &Error{msg: fmt.Sprintf(format, a...)}
}

// Load reads and validates a YAML config file at path.
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errf("failed to read config file: %v", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, errf("failed to parse YAML: %v", err)
	}
	if raw == nil {
		return nil, errf("configuration must be a top-level mapping")
	}
	return Build(raw)
}

// Build validates a raw map (from YAML or admin JSON) and returns an AppConfig.
// API is the encoding-neutral validation used by both file load and hot-reload.
func Build(raw map[string]any) (*AppConfig, error) {
	if raw == nil {
		return nil, errf("configuration must be a top-level mapping")
	}
	return buildConfig(raw)
}

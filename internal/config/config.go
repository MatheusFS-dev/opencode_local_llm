package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config stores the user-owned local model settings.
type Config struct {
	ModelDirectory string `json:"model_directory"`
	SelectedModel  string `json:"selected_model"`
	ContextLength  int    `json:"context_length"`
}

// Default returns an initial configuration that requires a user-selected model directory.
func Default(_ string) Config {
	return Config{
		ContextLength: 16384,
	}
}

// Load reads one configuration file or returns defaults when it does not exist.
func Load(path string, defaults Config) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaults, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read local LLM configuration: %w", err)
	}
	var value Config
	if err := json.Unmarshal(data, &value); err != nil {
		return Config{}, fmt.Errorf("parse local LLM configuration: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Config{}, err
	}
	return value, nil
}

// Save validates and atomically writes one configuration file.
func Save(path string, value Config) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create local LLM configuration directory: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local LLM configuration: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temporary local LLM configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write local LLM configuration: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set local LLM configuration permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close local LLM configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace local LLM configuration: %w", err)
	}
	return nil
}

// Validate checks the settings that affect server ownership and storage.
func (value Config) Validate() error {
	if value.ModelDirectory != "" && !filepath.IsAbs(value.ModelDirectory) {
		return fmt.Errorf("model directory must be an absolute path")
	}
	if value.ContextLength != 16384 && value.ContextLength != 32768 {
		return fmt.Errorf("context length must be 16384 or 32768")
	}
	return nil
}

package models

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Entry is one downloadable Ollama model presented by the dashboard.
type Entry struct {
	Name string `json:"name"`
	Size string `json:"size"`
	VRAM string `json:"vram"`
}

// Load reads and validates the editable model catalog.
func Load(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model catalog: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse model catalog: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("model catalog is empty")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			return nil, fmt.Errorf("model catalog contains an empty model name")
		}
		if seen[entry.Name] {
			return nil, fmt.Errorf("model catalog repeats model %q", entry.Name)
		}
		seen[entry.Name] = true
	}
	return entries, nil
}

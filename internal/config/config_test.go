package config

import (
	"path/filepath"
	"testing"
)

func TestLoadUsesSharedDataDirectoryAndPersistsSelectedModel(t *testing.T) {
	dataRoot := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.json")
	defaults := Default(dataRoot)

	if defaults.ModelDirectory != filepath.Join(dataRoot, "opencode-local-llm", "models") {
		t.Fatalf("model directory = %q", defaults.ModelDirectory)
	}
	if defaults.ContextLength != 16384 {
		t.Fatalf("context length = %d", defaults.ContextLength)
	}
	defaults.SelectedModel = "huihui_ai/qwen3.5-abliterated:4B"
	if err := Save(path, defaults); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path, Default(dataRoot))
	if err != nil {
		t.Fatal(err)
	}
	if loaded != defaults {
		t.Fatalf("loaded = %#v, want %#v", loaded, defaults)
	}
}

func TestLoadRejectsUnsafeModelDirectoryAndContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{ModelDirectory: "relative", ContextLength: 123}); err == nil {
		t.Fatal("Save succeeded for invalid configuration")
	}
}

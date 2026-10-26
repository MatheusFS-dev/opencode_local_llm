package config

import (
	"path/filepath"
	"testing"
)

func TestLoadRequiresUserSelectedModelDirectoryAndPersistsSelectedModel(t *testing.T) {
	dataRoot := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.json")
	defaults := Default(dataRoot)

	if defaults.ModelDirectory != "" {
		t.Fatalf("model directory = %q, want empty", defaults.ModelDirectory)
	}
	if defaults.ContextLength != 16384 {
		t.Fatalf("context length = %d", defaults.ContextLength)
	}
	configured := defaults
	configured.ModelDirectory = filepath.Join(dataRoot, "models")
	configured.SelectedModel = "huihui_ai/qwen3.5-abliterated:4B"
	if err := Save(path, configured); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path, Default(dataRoot))
	if err != nil {
		t.Fatal(err)
	}
	if loaded != configured {
		t.Fatalf("loaded = %#v, want %#v", loaded, configured)
	}
}

func TestLoadRejectsUnsafeModelDirectoryAndContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{ModelDirectory: "relative", ContextLength: 123}); err == nil {
		t.Fatal("Save succeeded for invalid configuration")
	}
}

func TestSaveAcceptsUnconfiguredModelDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Config{ContextLength: 16384}); err != nil {
		t.Fatalf("Save rejected an unconfigured model directory: %v", err)
	}
}

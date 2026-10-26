package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogAcceptsExistingSixModelCatalog(t *testing.T) {
	catalog, err := Load(filepath.Join("..", "..", "src", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 6 {
		t.Fatalf("models = %d, want 6", len(catalog))
	}
}

func TestLoadCatalogRejectsDuplicateAndBlankNames(t *testing.T) {
	for name, data := range map[string]string{
		"duplicate": `[{"name":"one","size":"1 GB"},{"name":"one","size":"2 GB"}]`,
		"blank":     `[{"name":"","size":"1 GB"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "models.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load succeeded")
			}
		})
	}
}

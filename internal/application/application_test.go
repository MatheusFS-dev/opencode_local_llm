package application

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

func TestRunWorkerUsesPersistedSelectedModel(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := config.Save(configPath, config.Config{ModelDirectory: filepath.Join(root, "models"), SelectedModel: "local:4b", ContextLength: 16384}); err != nil {
		t.Fatal(err)
	}
	var received worker.Request
	started := false
	application := Application{
		ConfigPath: configPath,
		Defaults:   config.Default(root),
		EnsureServer: func(config.Config) error {
			started = true
			return nil
		},
		ExecuteWorker: func(_ context.Context, request worker.Request, _, _ io.Writer) error {
			received = request
			return nil
		},
	}
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !started || received.Model != "local:4b" || received.Project != root {
		t.Fatalf("started=%t request=%#v", started, received)
	}
}

func TestRunWorkerRejectsMissingSelectedModel(t *testing.T) {
	root := t.TempDir()
	application := Application{ConfigPath: filepath.Join(root, "missing.json"), Defaults: config.Default(root)}
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("RunWorker succeeded")
	}
}

func TestRunWorkerRejectsSelectedModelRemovedOutsideDashboard(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := config.Save(configPath, config.Config{ModelDirectory: filepath.Join(root, "models"), SelectedModel: "removed:4b", ContextLength: 16384}); err != nil {
		t.Fatal(err)
	}
	application := Application{
		ConfigPath: configPath,
		Defaults:   config.Default(root),
		ValidateSelectedModel: func(context.Context, string) error {
			return fmt.Errorf("not installed")
		},
	}
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("RunWorker succeeded")
	}
}

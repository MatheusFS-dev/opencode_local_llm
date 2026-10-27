package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria", "--verify", "test -f deliverable.sh"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !started || received.Model != "local:4b" || received.Project != root {
		t.Fatalf("started=%t request=%#v", started, received)
	}
}

func TestWorkerRequiresIndependentAcceptanceCheck(t *testing.T) {
	for _, test := range []struct {
		name, child, verify, wantError string
	}{
		{"answer without file", "exit 0", "test -f deliverable.sh", "acceptance check failed"},
		{"file created", "touch deliverable.sh", "test -f deliverable.sh", ""},
		{"worker failed", "exit 7", "touch verification-ran", "exit status 7"},
		{"missing check", "touch deliverable.sh", "", "verification command is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "opencode")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+test.child+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			app := workerTestApplication(t, root, binary, 16384)
			var output bytes.Buffer
			err := app.RunWorker(t.Context(), []string{"--project", root, "--task", "Create deliverable.sh", "--criteria", "File exists", "--verify", test.verify}, &output, &output)
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if _, err := os.Stat(filepath.Join(root, "verification-ran")); !os.IsNotExist(err) {
				t.Fatal("verification ran after worker failure")
			}
		})
	}
}

func TestWorkerPassesRuntimeContextToOpenCode(t *testing.T) {
	for _, window := range []int{16384, 32768} {
		t.Run(fmt.Sprint(window), func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "opencode")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$OPENCODE_CONFIG_CONTENT\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("OPENCODE_CONFIG_CONTENT", `{"tool_output":{"max_lines":100}}`)
			app := workerTestApplication(t, root, binary, window)
			var output bytes.Buffer
			if err := app.RunWorker(t.Context(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria", "--verify", "true"}, &output, &output); err != nil {
				t.Fatal(err)
			}
			var settings struct {
				Providers map[string]struct {
					Models map[string]struct {
						Limit struct{ Context, Input, Output int }
					}
				}
				Compaction struct {
					Auto   bool
					Buffer int
				}
				ToolOutput struct {
					MaxLines int `json:"max_lines"`
				} `json:"tool_output"`
			}
			if err := json.Unmarshal(output.Bytes(), &settings); err != nil {
				t.Fatal(err)
			}
			limit := settings.Providers["ollama"].Models["local:4b"].Limit
			if limit.Context != window || limit.Input+limit.Output > window || limit.Output < 4096 || !settings.Compaction.Auto || settings.Compaction.Buffer < 2048 || settings.ToolOutput.MaxLines != 100 {
				t.Fatalf("unsafe or lost settings: %s", output.String())
			}
		})
	}
}

func workerTestApplication(t *testing.T, root, binary string, window int) Application {
	t.Helper()
	path := filepath.Join(root, "settings.json")
	if err := config.Save(path, config.Config{ModelDirectory: filepath.Join(root, "models"), SelectedModel: "local:4b", ContextLength: window}); err != nil {
		t.Fatal(err)
	}
	return Application{ConfigPath: path, Defaults: config.Default(root), ExecuteWorker: func(ctx context.Context, request worker.Request, out, stderr io.Writer) error {
		return worker.Run(ctx, binary, request, out, stderr)
	}}
}

func TestRunWorkerRejectsMissingSelectedModel(t *testing.T) {
	root := t.TempDir()
	application := Application{ConfigPath: filepath.Join(root, "missing.json"), Defaults: config.Default(root)}
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria", "--verify", "test -f deliverable.sh"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
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
	if err := application.RunWorker(context.Background(), []string{"--project", root, "--task", "Task", "--criteria", "Criteria", "--verify", "test -f deliverable.sh"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("RunWorker error = %v, want unavailable model", err)
	}
}

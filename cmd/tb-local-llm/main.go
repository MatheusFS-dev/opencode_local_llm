package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/application"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/server"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/ui"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tb-local-llm ERROR:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dataRoot, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(configRoot, "opencode-local-llm", "config.json")
	statePath := filepath.Join(configRoot, "opencode-local-llm", "server.json")
	defaults := config.Default(filepath.Join(dataRoot, ".local", "share"))
	if len(arguments) > 0 && arguments[0] == "worker" {
		ollamaPath, err := exec.LookPath("ollama")
		if err != nil {
			return fmt.Errorf("Ollama is missing; run tb local-llm and choose Set up Ollama")
		}
		openCodePath, err := exec.LookPath("opencode")
		if err != nil {
			return fmt.Errorf("OpenCode is missing; run tb install-opencode")
		}
		app := application.Application{
			ConfigPath: settingsPath,
			Defaults:   defaults,
			EnsureServer: func(settings config.Config) error {
				return (server.Manager{StatePath: statePath}).Ensure(context.Background(), ollamaPath, settings)
			},
			ExecuteWorker: func(ctx context.Context, request worker.Request, output, errorOutput io.Writer) error {
				return worker.Run(ctx, openCodePath, request, output, errorOutput)
			},
		}
		return app.RunWorker(context.Background(), arguments[1:], os.Stdout, os.Stderr)
	}
	catalogPath := filepath.Join(filepath.Dir(os.Args[0]), "models.json")
	catalog, err := models.Load(catalogPath)
	if err != nil {
		return err
	}
	saved, err := config.Load(settingsPath, defaults)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(ui.New(saved, catalog)).Run()
	return err
}

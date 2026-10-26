package application

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

// Application provides the noninteractive local-LLM command interfaces.
type Application struct {
	ConfigPath    string
	Defaults      config.Config
	EnsureServer  func(config.Config) error
	ExecuteWorker func(context.Context, worker.Request, io.Writer, io.Writer) error
}

// RunWorker validates worker flags, loads the selected model, and invokes OpenCode.
func (application Application) RunWorker(ctx context.Context, arguments []string, output, errorOutput io.Writer) error {
	flags := flag.NewFlagSet("worker", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	project := flags.String("project", "", "project directory")
	task := flags.String("task", "", "implementation task")
	criteria := flags.String("criteria", "", "acceptance criteria")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("worker does not accept positional arguments")
	}
	saved, err := config.Load(application.ConfigPath, application.Defaults)
	if err != nil {
		return err
	}
	if saved.SelectedModel == "" {
		return fmt.Errorf("no local model is selected; run tb local-llm and select an installed model")
	}
	request := worker.Request{Project: *project, Task: *task, Criteria: *criteria, Model: saved.SelectedModel}
	if _, err := worker.BuildArguments(request); err != nil {
		return err
	}
	if application.EnsureServer != nil {
		if err := application.EnsureServer(saved); err != nil {
			return fmt.Errorf("ensure local Ollama server: %w", err)
		}
	}
	if application.ExecuteWorker == nil {
		return fmt.Errorf("OpenCode worker runner is unavailable")
	}
	return application.ExecuteWorker(ctx, request, output, errorOutput)
}

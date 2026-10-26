package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Request defines one OpenCode implementation task.
type Request struct {
	Project  string
	Task     string
	Criteria string
	Model    string
}

// BuildArguments validates a request and returns an argument-safe OpenCode invocation.
func BuildArguments(request Request) ([]string, error) {
	if strings.TrimSpace(request.Project) == "" {
		return nil, fmt.Errorf("project directory is required")
	}
	if strings.TrimSpace(request.Task) == "" {
		return nil, fmt.Errorf("task is required")
	}
	if strings.TrimSpace(request.Criteria) == "" {
		return nil, fmt.Errorf("acceptance criteria are required")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, fmt.Errorf("select an installed local model before running a worker")
	}
	prompt := fmt.Sprintf("Implement: %s\n\nAcceptance criteria: %s\n\nRun relevant tests. Do not commit or push.", request.Task, request.Criteria)
	return []string{"run", "--standalone", "--agent", "build", "--model", "ollama/" + request.Model, prompt}, nil
}

// Run invokes OpenCode in the requested project and preserves child output.
func Run(ctx context.Context, binary string, request Request, output, errorOutput io.Writer) error {
	arguments, err := BuildArguments(request)
	if err != nil {
		return err
	}
	info, err := os.Stat(request.Project)
	if err != nil {
		return fmt.Errorf("inspect project directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory: %s", request.Project)
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = request.Project
	command.Stdout = output
	command.Stderr = errorOutput
	if err := command.Run(); err != nil {
		return fmt.Errorf("run OpenCode worker: %w", err)
	}
	return nil
}

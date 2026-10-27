package maestro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

// WorkerServer exposes durable implementation jobs to native interactive clients.
type WorkerServer struct {
	Store    Store
	Root     string
	Settings func() (config.Config, error)
	Start    func(Job) error
}

func (server WorkerServer) authorize(project string) error {
	if server.Root == "" {
		return fmt.Errorf("Maestro session working directory is required")
	}
	root, err := filepath.EvalSymlinks(server.Root)
	if err != nil {
		return err
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, project)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("project is outside this Maestro session's working directory; start a Maestro session in the requested project")
	}
	return nil
}

// Call executes a worker lifecycle operation without invoking another controller.
func (server WorkerServer) Call(name string, arguments json.RawMessage) (string, error) {
	var input struct {
		Project    string            `json:"project"`
		Task       string            `json:"task"`
		Criteria   string            `json:"criteria"`
		Verify     string            `json:"verify"`
		Artifacts  []worker.Artifact `json:"artifacts"`
		Timeout    int               `json:"timeout_seconds"`
		ID         string            `json:"job_id"`
		Approved   *bool             `json:"approved"`
		Reason     string            `json:"reason"`
		Correction string            `json:"correction"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return "", err
	}
	encode := func(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }
	if name == "start_implementation" {
		if !filepath.IsAbs(input.Project) {
			return "", fmt.Errorf("project must be an absolute existing directory")
		}
		if err := server.authorize(input.Project); err != nil {
			return "", err
		}
		info, err := os.Stat(input.Project)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("project must be an absolute existing directory")
		}
		plan := Plan{Task: input.Task, Criteria: input.Criteria, Verify: input.Verify, Artifacts: input.Artifacts}
		if err := plan.Validate(); err != nil {
			return "", err
		}
		if err := worker.ValidateArtifacts(input.Artifacts, []string{input.Project}); err != nil {
			return "", err
		}
		if server.Settings == nil || server.Start == nil {
			return "", fmt.Errorf("worker service unavailable")
		}
		settings, err := server.Settings()
		if err != nil {
			return "", err
		}
		if input.Timeout == 0 {
			input.Timeout = 1800
		}
		job, err := server.Store.Create(worker.Request{Project: input.Project, Task: input.Task, Criteria: input.Criteria, Verify: input.Verify, Artifacts: input.Artifacts, AllowedArtifactRoots: []string{input.Project}, Model: settings.SelectedModel, ContextLength: settings.ContextLength}, settings, "interactive", time.Duration(input.Timeout)*time.Second)
		if err != nil {
			return "", err
		}
		job.UserTask = input.Task
		if err = server.Store.Save(job); err != nil {
			return "", err
		}
		if err = server.Start(job); err != nil {
			job.Status, job.Error = Failed, err.Error()
			_ = server.Store.Save(job)
			return "", err
		}
		return encode(job)
	}
	job, err := server.Store.Load(input.ID)
	if err != nil {
		return "", err
	}
	if job.Provider != "interactive" {
		return "", fmt.Errorf("job belongs to a separate supervised controller")
	}
	if err := server.authorize(job.Request.Project); err != nil {
		return "", err
	}
	switch name {
	case "job_status", "wait_for_job":
		duration := time.Millisecond
		if name == "wait_for_job" {
			duration = 20 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		defer cancel()
		current, err := server.Store.Wait(ctx, job.ID, nil)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return encode(current)
	case "job_logs":
		directory, _ := server.Store.Directory(job.ID)
		file, err := os.Open(filepath.Join(directory, "worker.log"))
		if os.IsNotExist(err) && job.Status == Queued {
			return "Worker has not produced output yet. Continue waiting for the recorded result.", nil
		}
		if err != nil {
			return "", err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		prefix := ""
		if info.Size() > 65536 {
			if _, err := file.Seek(-65536, io.SeekEnd); err != nil {
				return "", err
			}
			prefix = "[Showing final 64 KiB. Full raw log: " + file.Name() + "]\n"
		}
		data, err := io.ReadAll(io.LimitReader(file, 65536))
		return prefix + string(data), err
	case "review_job":
		if input.Approved == nil {
			return "", fmt.Errorf("explicit approved decision is required")
		}
		lock, err := server.Store.LockController(job.ID)
		if err != nil {
			return "", err
		}
		defer lock.Close()
		job, err = server.Store.Load(job.ID)
		if err != nil {
			return "", err
		}
		if !*input.Approved && strings.TrimSpace(input.Correction) == "" {
			return "", fmt.Errorf("rejected review requires a focused correction")
		}
		if job.Status != ChangesRequested {
			reason := input.Reason
			if !*input.Approved {
				reason += "\nFocused correction: " + input.Correction
			}
			if err := server.Store.Review(job.ID, *input.Approved, reason); err != nil {
				return "", err
			}
			job, err = server.Store.Load(job.ID)
			if err != nil {
				return "", err
			}
		}
		if job.Status == ChangesRequested {
			if *input.Approved {
				return "", fmt.Errorf("rejected job requires review of its correction")
			}
			child, err := server.Store.Correction(job, 30*time.Minute)
			if err != nil {
				return "", err
			}
			if child.Status == Queued && child.PID == 0 {
				if server.Start == nil {
					return "", fmt.Errorf("worker service unavailable")
				}
				if err := server.Start(child); err != nil {
					child.Status, child.Error = Failed, err.Error()
					_ = server.Store.Save(child)
					return "", err
				}
			}
			return encode(child)
		}
		return encode(job)
	default:
		return "", fmt.Errorf("unsupported worker tool %q", name)
	}
}

func workerTools() []any {
	stringProperty := map[string]any{"type": "string"}
	tools := []any{}
	for _, name := range []string{"start_implementation", "job_status", "wait_for_job", "job_logs", "review_job"} {
		properties := map[string]any{"job_id": stringProperty}
		required := []string{"job_id"}
		description := "Read the recorded worker result. Running or quiet jobs must be awaited, never classified as failed."
		if name == "start_implementation" {
			properties = map[string]any{"project": stringProperty, "task": stringProperty, "criteria": stringProperty, "verify": map[string]any{"type": "string", "description": "Executable POSIX shell acceptance command, never prose. For a new executable Bash script: bash -n ./script.sh && test -x ./script.sh. Runs from project after implementation."}, "timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 86400}, "artifacts": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{"path": stringProperty, "executable": map[string]any{"type": "boolean"}}, "required": []string{"path", "executable"}, "additionalProperties": false}}}
			required = []string{"project", "task", "criteria", "verify", "artifacts"}
			description = "Delegate implementation and test execution to local OpenCode. Returns immediately with a durable job ID. Use the smallest existing project directory containing all deliverables. Then repeatedly wait_for_job until a terminal result or awaiting_review."
		}
		if name == "review_job" {
			properties["approved"] = map[string]any{"type": "boolean"}
			properties["reason"] = stringProperty
			properties["correction"] = stringProperty
			required = append(required, "approved", "reason")
			description = "Record review after reading actual files and test output. Approve verified work or request one focused correction. At most three rounds. Failed jobs cannot be retried or approved."
		}
		tools = append(tools, map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": name != "start_implementation" && name != "review_job", "destructiveHint": false}})
	}
	return tools
}

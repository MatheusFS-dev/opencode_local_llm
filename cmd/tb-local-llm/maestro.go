package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/application"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/maestro"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/server"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/terminal"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

func workerApplication(settingsPath, statePath string, defaults config.Config) (application.Application, error) {
	ollamaPath, err := exec.LookPath("ollama")
	if err != nil {
		return application.Application{}, fmt.Errorf("Ollama is missing; run tb local-llm and choose Set up Ollama")
	}
	codePath, err := openCodePath()
	if err != nil {
		return application.Application{}, fmt.Errorf("OpenCode is missing; run tb install-opencode")
	}
	return application.Application{
		ConfigPath: settingsPath, Defaults: defaults,
		EnsureServer: func(settings config.Config) error {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			return (server.Manager{StatePath: statePath}).Ensure(ctx, ollamaPath, settings)
		},
		ValidateSelectedModel: func(ctx context.Context, selected string) error {
			query, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			installed, err := ollamaClient().Installed(query)
			if err != nil {
				return err
			}
			for _, model := range installed {
				if model.Name == selected {
					return nil
				}
			}
			return fmt.Errorf("%s is not installed; choose or download it in tb local-llm", selected)
		},
		ExecuteWorker: func(ctx context.Context, request worker.Request, out, stderr io.Writer) error {
			return worker.Run(ctx, codePath, request, out, stderr)
		},
	}, nil
}

type contextRoots []string

func (roots *contextRoots) String() string         { return strings.Join(*roots, ", ") }
func (roots *contextRoots) Set(value string) error { *roots = append(*roots, value); return nil }

func runControllerHelper(arguments []string) error {
	if arguments[0] == "maestro-guard" {
		flags := flag.NewFlagSet("maestro-guard", flag.ContinueOnError)
		provider := flags.String("provider", "", "hook protocol")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		return maestro.GuardForProvider(os.Stdin, os.Stdout, *provider)
	}
	flags := flag.NewFlagSet("maestro-context", flag.ContinueOnError)
	var roots contextRoots
	flags.Var(&roots, "root", "authorized read-only directory (repeatable)")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if len(roots) == 0 || flags.NArg() != 0 {
		return fmt.Errorf("maestro-context requires --root and no positional arguments")
	}
	return (maestro.ContextReader{Roots: roots}).Serve(os.Stdin, os.Stdout)
}

func runMaestroCLI(arguments []string, settingsPath, statePath string, defaults config.Config) error {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		root = filepath.Join(home, ".local", "state")
	}
	store := maestro.Store{Root: filepath.Join(root, "opencode-local-llm", "jobs")}
	if arguments[0] == "maestro-mcp" {
		if len(arguments) != 1 {
			return fmt.Errorf("maestro-mcp accepts no arguments")
		}
		self, err := os.Executable()
		if err != nil {
			return err
		}
		current, err := os.Getwd()
		if err != nil {
			return err
		}
		service := maestro.WorkerServer{Root: current, Store: store, Settings: func() (config.Config, error) { return config.Load(settingsPath, defaults) }, Start: func(job maestro.Job) error { return store.Start(self, job) }}
		return (maestro.ContextReader{Roots: []string{current}, Worker: &service}).Serve(os.Stdin, os.Stdout)
	}
	output := terminal.Output{Writer: os.Stdout, Tool: "local-llm"}
	if arguments[0] == "job" {
		return runJobCommand(store, arguments[1:], &output)
	}
	if arguments[0] == "maestro-job" {
		flags := flag.NewFlagSet("maestro-job", flag.ContinueOnError)
		root := flags.String("store", store.Root, "job store")
		id := flags.String("id", "", "job ID")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		store.Root = *root
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return store.Execute(ctx, *id, func(ctx context.Context, job maestro.Job, log io.Writer, verifying func() error) error {
			app, err := workerApplication(settingsPath, statePath, defaults)
			if err != nil {
				return err
			}
			job.Request.OnVerifying = verifying
			return app.RunRequest(ctx, job.Settings, job.Request, log, log)
		})
	}
	flags := flag.NewFlagSet("maestro", flag.ContinueOnError)
	providerName := flags.String("provider", "codex", "planner/reviewer: codex, claude, or antigravity")
	project := flags.String("project", "", "project directory (default: current directory)")
	task := flags.String("task", "", "coding task (required unless resuming; prompts when omitted)")
	resume := flags.String("resume", "", "reconnect to an existing job ID")
	timeout := flags.Duration("timeout", 30*time.Minute, "deadline per worker or provider phase (maximum 24h)")
	var allowedRoots contextRoots
	flags.Var(&allowedRoots, "allow-write", "explicit deliverable directory outside the project (repeatable)")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("maestro does not accept positional arguments; use --task")
	}
	if *timeout <= 0 || *timeout > 24*time.Hour {
		return fmt.Errorf("timeout must be greater than zero and at most 24h")
	}
	if *providerName == "agy" {
		*providerName = "antigravity"
	}
	providerSpecified := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "provider" {
			providerSpecified = true
		}
	})
	if *resume != "" {
		job, err := store.Load(*resume)
		if err != nil {
			return err
		}
		if providerSpecified && *providerName != job.Provider {
			return fmt.Errorf("job %s must resume with its original provider %s", job.ID, job.Provider)
		}
		*project, *providerName = job.Request.Project, job.Provider
		allowedRoots = job.Request.AllowedArtifactRoots
	}
	if *providerName != "codex" && *providerName != "claude" && *providerName != "antigravity" {
		return fmt.Errorf("provider must be codex, claude, or antigravity")
	}
	if *project == "" {
		current, err := os.Getwd()
		if err != nil {
			return err
		}
		*project = current
	}
	absolute, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	*project, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if info, err := os.Stat(*project); err != nil || !info.IsDir() {
		return fmt.Errorf("project must be an existing directory")
	}
	for index, root := range allowedRoots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("authorized directory is unavailable: %w", err)
		}
		if info, err := os.Stat(canonical); err != nil || !info.IsDir() {
			return fmt.Errorf("--allow-write requires an existing directory")
		}
		allowedRoots[index] = canonical
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return fmt.Errorf("Maestro requires Bubblewrap for read-only provider isolation; install bubblewrap and retry")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return fmt.Errorf("Maestro requires ripgrep for read-only context searches; install ripgrep and retry")
	}
	binaryName := *providerName
	if binaryName == "antigravity" {
		binaryName = "agy"
	}
	binary, err := exec.LookPath(binaryName)
	if err != nil {
		return fmt.Errorf("%s is unavailable; run tb install-%s", binaryName, *providerName)
	}
	settings, err := config.Load(settingsPath, defaults)
	if err != nil {
		return err
	}
	if *resume == "" && settings.SelectedModel == "" {
		return fmt.Errorf("select an installed local model in tb local-llm before starting Maestro")
	}
	if *resume == "" && strings.TrimSpace(*task) == "" {
		scanner := bufio.NewScanner(os.Stdin)
		for strings.TrimSpace(*task) == "" {
			if err := output.Message("PROMPT", "Coding task (required):"); err != nil {
				return err
			}
			if !scanner.Scan() {
				return fmt.Errorf("a coding task is required")
			}
			*task = scanner.Text()
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.Root, 0o700); err != nil {
		return err
	}
	session, err := os.MkdirTemp(store.Root, "session-")
	if err != nil {
		return err
	}
	controller := maestro.Provider{Name: *providerName, Binary: binary, Self: self, Project: *project, Directory: session, ReadRoots: allowedRoots}
	supervisor := maestro.Supervisor{Store: store, Project: *project, Provider: *providerName, Settings: settings, AllowedRoots: allowedRoots, Controller: controller, Timeout: *timeout,
		Start:    func(job maestro.Job) error { return store.Start(self, job) },
		Announce: func(message string) { _ = output.Message("INFO", message) },
		Progress: func(job maestro.Job) {
			_ = output.Message("INFO", fmt.Sprintf("Job %s: %s, %d seconds elapsed. Waiting for its recorded result; quiet output is not a failure.", job.ID, job.Status, int(time.Since(job.Created).Seconds())))
		},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	job, err := supervisor.Run(ctx, *task, *resume)
	if err != nil {
		return err
	}
	return output.Message("INFO", fmt.Sprintf("Job %s completed after independent verification and %s review: %s", job.ID, job.Provider, job.Review))
}

func runJobCommand(store maestro.Store, arguments []string, output *terminal.Output) error {
	if len(arguments) < 2 {
		return fmt.Errorf("usage: tb local-llm job status|logs|cancel <job-id> [--json]")
	}
	job, err := store.Load(arguments[1])
	if err != nil {
		return err
	}
	directory, _ := store.Directory(job.ID)
	switch arguments[0] {
	case "status":
		if len(arguments) == 3 && arguments[2] == "--json" {
			return json.NewEncoder(os.Stdout).Encode(job)
		}
		if len(arguments) != 2 {
			return fmt.Errorf("usage: job status <job-id> [--json]")
		}
		return output.Message("INFO", fmt.Sprintf("Job %s: %s; model %s; deadline %s; error %s; correction job %s; logs %s", job.ID, job.Status, job.Request.Model, job.Deadline.Format(time.RFC3339), job.Error, job.ChildID, directory))
	case "logs":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: job logs <job-id>")
		}
		file, err := os.Open(filepath.Join(directory, "worker.log"))
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(os.Stdout, file)
		return err
	case "cancel":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: job cancel <job-id>")
		}
		if err := store.Cancel(job.ID); err != nil {
			return err
		}
		return output.Message("INFO", "Cancellation requested for job "+job.ID+". Its runner will record the final outcome.")
	default:
		return fmt.Errorf("unknown job operation %q", arguments[0])
	}
}

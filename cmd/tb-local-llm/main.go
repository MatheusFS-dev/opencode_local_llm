package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/application"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/ollama"
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
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if dataRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dataRoot = filepath.Join(home, ".local", "share")
	}
	settingsPath := filepath.Join(configRoot, "opencode-local-llm", "config.json")
	statePath := filepath.Join(configRoot, "opencode-local-llm", "server.json")
	defaults := config.Default(dataRoot)
	if len(arguments) > 0 && arguments[0] == "worker" {
		ollamaPath, err := exec.LookPath("ollama")
		if err != nil {
			return fmt.Errorf("Ollama is missing; run tb local-llm and choose Set up Ollama")
		}
		openCodePath, err := openCodePath()
		if err != nil {
			return fmt.Errorf("OpenCode is missing; run tb install-opencode")
		}
		app := application.Application{
			ConfigPath: settingsPath,
			Defaults:   defaults,
			EnsureServer: func(settings config.Config) error {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				return (server.Manager{StatePath: statePath}).Ensure(ctx, ollamaPath, settings)
			},
			ValidateSelectedModel: func(ctx context.Context, selected string) error {
				installed, err := ollamaClient().Installed(ctx)
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
	manager := server.Manager{StatePath: statePath}
	ollamaPath := func() string {
		path, _ := exec.LookPath("ollama")
		return path
	}
	ensure := func() error {
		if saved.ModelDirectory == "" {
			return fmt.Errorf("model storage is not configured; choose Configure model storage")
		}
		path := ollamaPath()
		if path == "" {
			return fmt.Errorf("Ollama is missing; choose Set up Ollama")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		return manager.Ensure(ctx, path, saved)
	}
	client := ollamaClient()
	actions := ui.Actions{
		Setup: func() error {
			if path := ollamaPath(); path != "" {
				return fmt.Errorf("Ollama is already installed at %s", path)
			}
			return installOllama()
		},
		Start: ensure,
		Stop:  manager.Stop,
		Reset: func() (config.Config, error) {
			if err := manager.Stop(); err != nil && !strings.Contains(err.Error(), "no managed Ollama server is running") {
				return config.Config{}, err
			}
			saved = defaults
			if err := config.Save(settingsPath, saved); err != nil {
				return config.Config{}, err
			}
			return saved, nil
		},
		SetStorage: func(directory string) (config.Config, error) {
			if !filepath.IsAbs(directory) {
				return config.Config{}, fmt.Errorf("model storage directory must be an absolute path")
			}
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return config.Config{}, fmt.Errorf("create model storage directory: %w", err)
			}
			info, err := os.Stat(directory)
			if err != nil || !info.IsDir() {
				return config.Config{}, fmt.Errorf("model storage path is not a directory: %s", directory)
			}
			saved.ModelDirectory = directory
			if err := config.Save(settingsPath, saved); err != nil {
				return config.Config{}, err
			}
			return saved, nil
		},
		Download: func(model string) error {
			if err := ensure(); err != nil {
				return err
			}
			return client.Pull(context.Background(), model, nil)
		},
		Delete: func(model string) error {
			if err := ensure(); err != nil {
				return err
			}
			if err := client.Delete(context.Background(), model); err != nil {
				return err
			}
			if saved.SelectedModel == model {
				saved.SelectedModel = ""
				return config.Save(settingsPath, saved)
			}
			return nil
		},
		Select: func(model string) error {
			saved.SelectedModel = model
			return config.Save(settingsPath, saved)
		},
		Launch: func(project string) (*exec.Cmd, error) {
			if saved.SelectedModel == "" {
				return nil, fmt.Errorf("select an installed model first")
			}
			info, err := os.Stat(project)
			if err != nil || !info.IsDir() {
				return nil, fmt.Errorf("project folder is unavailable: %s", project)
			}
			if err := ensure(); err != nil {
				return nil, err
			}
			path, err := openCodePath()
			if err != nil {
				return nil, err
			}
			command := exec.Command(path, "--model", "ollama/"+saved.SelectedModel)
			command.Dir = project
			return command, nil
		},
		Refresh: func() ui.Snapshot { return snapshot(client) },
	}
	_, err = tea.NewProgram(ui.New(saved, catalog, actions)).Run()
	return err
}

func ollamaClient() *ollama.Client {
	return ollama.New("http://127.0.0.1:11434", &http.Client{Timeout: 2 * time.Second})
}

func snapshot(client *ollama.Client) ui.Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot := ui.Snapshot{Server: "stopped", GPU: gpuStatus(), LoadedVRAM: "Unavailable", Installed: map[string]bool{}}
	if err := client.Healthy(ctx); err != nil {
		return snapshot
	}
	snapshot.Server = "running"
	if installed, err := client.Installed(ctx); err == nil {
		for _, model := range installed {
			snapshot.Installed[model.Name] = true
		}
	}
	if loaded, err := client.Loaded(ctx); err == nil && len(loaded) > 0 {
		snapshot.LoadedVRAM = bytes(loaded[0].VRAMBytes)
	}
	return snapshot
}

func gpuStatus() string {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return "Unavailable"
	}
	output, err := exec.Command(path, "--query-gpu=name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return "Unavailable"
	}
	return formatGPUStatus(string(output))
}

func formatGPUStatus(output string) string {
	entries := make([]string, 0)
	for _, row := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Split(row, ",")
		if len(fields) != 4 {
			return "Unavailable"
		}
		name := strings.TrimSpace(fields[0])
		utilization, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil || name == "" {
			return "Unavailable"
		}
		used, err := formatMiB(strings.TrimSpace(fields[2]))
		if err != nil {
			return "Unavailable"
		}
		total, err := formatMiB(strings.TrimSpace(fields[3]))
		if err != nil {
			return "Unavailable"
		}
		entries = append(entries, fmt.Sprintf("GPU: %s\n%d%% utilization, %s MiB / %s MiB VRAM used", name, utilization, used, total))
	}
	if len(entries) == 0 {
		return "Unavailable"
	}
	return strings.Join(entries, "\n\n")
}

func formatMiB(value string) (string, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 {
		return "", fmt.Errorf("invalid MiB value %q", value)
	}
	formatted := strconv.FormatInt(number, 10)
	for index := len(formatted) - 3; index > 0; index -= 3 {
		formatted = formatted[:index] + "," + formatted[index:]
	}
	return formatted, nil
}

func bytes(value int64) string {
	if value <= 0 {
		return "Unavailable"
	}
	return fmt.Sprintf("%.1f GiB", float64(value)/(1024*1024*1024))
}

func installOllama() error {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return fmt.Errorf("curl is required to install Ollama")
	}
	download := exec.Command(curl, "-fsSL", "https://ollama.com/install.sh")
	script, err := download.Output()
	if err != nil {
		return fmt.Errorf("download official Ollama installer: %w", err)
	}
	command := exec.Command("bash")
	command.Stdin = strings.NewReader(string(script))
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run official Ollama installer: %w", err)
	}
	return nil
}

func openCodePath() (string, error) {
	if path, err := exec.LookPath("opencode"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".opencode", "bin", "opencode")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return "", fmt.Errorf("OpenCode is missing; run tb install-opencode")
}

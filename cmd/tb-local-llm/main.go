package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/ollama"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/server"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/terminal"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/ui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		output := terminal.Output{Writer: os.Stderr, Tool: "local-llm"}
		_ = output.Message("ERROR", err.Error())
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "repair-sandbox" {
		return runSandboxRepair()
	}
	if len(arguments) > 0 && (arguments[0] == "maestro-context" || arguments[0] == "maestro-guard") {
		return runControllerHelper(arguments)
	}
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
	if len(arguments) > 0 && (arguments[0] == "maestro" || arguments[0] == "maestro-job" || arguments[0] == "job" || arguments[0] == "maestro-mcp") {
		return runMaestroCLI(arguments, settingsPath, statePath, defaults)
	}
	if len(arguments) > 0 && arguments[0] == "worker" {
		app, err := workerApplication(settingsPath, statePath, defaults)
		if err != nil {
			return err
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
		RepairSandbox: func() (*exec.Cmd, error) {
			self, err := os.Executable()
			if err != nil {
				return nil, err
			}
			return exec.Command(self, "repair-sandbox"), nil
		},
		Setup: func() error {
			if path := ollamaPath(); path != "" {
				return fmt.Errorf("Ollama is already installed at %s", path)
			}
			return installOllama()
		},
		Start: ensure,
		ListModels: func() ([]string, error) {
			if err := ensure(); err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			installed, err := client.Installed(ctx)
			if err != nil {
				return nil, fmt.Errorf("query configured Ollama server: %w; retry Start managed Ollama server", err)
			}
			names := make([]string, 0, len(installed))
			for _, model := range installed {
				names = append(names, model.Name)
			}
			return names, nil
		},
		Stop: manager.Stop,
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
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if _, err := manager.StopAndWait(ctx); err != nil {
				return config.Config{}, fmt.Errorf("apply model storage configuration: %w", err)
			}
			return saved, nil
		},
		SetContext: func(length int) (config.Config, error) {
			updated := saved
			updated.ContextLength = length
			if err := config.Save(settingsPath, updated); err != nil {
				return config.Config{}, err
			}
			saved = updated
			return saved, nil
		},
		Download: func(model string, report func(ui.DownloadProgress)) error {
			if err := ensure(); err != nil {
				return err
			}
			if err := client.Pull(context.Background(), model, func(progress ollama.Progress) {
				if report != nil {
					report(ui.DownloadProgress{Status: progress.Status, Completed: progress.Completed, Total: progress.Total})
				}
			}); err != nil {
				return err
			}
			return verifyDownload(client, saved.ModelDirectory, model)
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
			return openCodeSessionCommand(path, project, saved.SelectedModel), nil
		},
		Refresh: func() ui.Snapshot { return snapshot(client) },
	}
	_, err = tea.NewProgram(ui.New(saved, catalog, actions)).Run()
	return err
}

func ollamaClient() *ollama.Client {
	return ollamaClientFor("http://127.0.0.1:11434")
}

func verifyDownload(client *ollama.Client, directory, model string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	installed, err := client.Installed(ctx)
	if err != nil {
		return fmt.Errorf("verify downloaded model: %w; restart the managed server and retry the download", err)
	}
	found := false
	for _, item := range installed {
		if item.Name == model || item.Name == model+":latest" {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("download verification failed: %s is absent from Ollama; restart the managed server and retry the download to %s", model, directory)
	}
	name, tag, tagged := strings.Cut(model, ":")
	if !tagged {
		tag = "latest"
	}
	if !strings.Contains(name, "/") {
		name = "library/" + name
	}
	manifestPath := filepath.Join(directory, "manifests", "registry.ollama.ai", filepath.FromSlash(name), tag)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("download storage mismatch: %s is listed by Ollama but its manifest is unavailable in %s: %w; configure this directory, restart the managed server, and retry the download", model, directory, err)
	}
	type blob struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	}
	var manifest struct {
		Config blob   `json:"config"`
		Layers []blob `json:"layers"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("invalid downloaded manifest: %w; retry the download", err)
	}
	if len(manifest.Layers) == 0 {
		return fmt.Errorf("downloaded manifest has no model layers; retry the download")
	}
	for _, entry := range append(manifest.Layers, manifest.Config) {
		if !strings.HasPrefix(entry.Digest, "sha256:") || strings.ContainsAny(entry.Digest, "/\\") || entry.Size <= 0 {
			return fmt.Errorf("invalid model blob metadata; retry the download")
		}
		info, err := os.Stat(filepath.Join(directory, "blobs", strings.ReplaceAll(entry.Digest, ":", "-")))
		if err != nil || !info.Mode().IsRegular() || info.Size() != entry.Size {
			return fmt.Errorf("download verification failed: missing or incomplete blob %s in %s; retry the download to repair model storage", entry.Digest, directory)
		}
	}
	return nil
}

func ollamaClientFor(baseURL string) *ollama.Client {
	return ollama.New(baseURL, &http.Client{})
}

func snapshot(client *ollama.Client) ui.Snapshot {
	gpu := gpuStatus()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot := ui.Snapshot{Server: "stopped", GPU: gpu, LoadedVRAM: "Unavailable", Installed: map[string]bool{}}
	if err := client.Healthy(ctx); err != nil {
		return snapshot
	}
	snapshot.Server = "running"
	if installed, err := client.Installed(ctx); err == nil {
		for _, model := range installed {
			snapshot.Installed[model.Name] = true
		}
	} else {
		snapshot.ModelError = err.Error()
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

func openCodeSessionCommand(path, project, model string) *exec.Cmd {
	settings, _ := json.Marshal(struct {
		Model string `json:"model"`
	}{Model: "ollama/" + model})
	command := exec.Command(path)
	command.Dir = project
	command.Env = append(os.Environ(), "OPENCODE_CONFIG_CONTENT="+string(settings))
	return command
}

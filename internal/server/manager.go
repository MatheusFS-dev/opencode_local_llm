package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
)

// State identifies an Ollama process launched by this application.
type State struct {
	PID            int    `json:"pid"`
	ModelDirectory string `json:"model_directory"`
	ContextLength  int    `json:"context_length"`
}

// Matches reports whether the server was started with the requested settings.
func (state State) Matches(settings config.Config) bool {
	return state.ModelDirectory == settings.ModelDirectory && state.ContextLength == settings.ContextLength
}

// Manager safely controls only an application-owned Ollama process.
type Manager struct {
	StatePath        string
	ProcessArguments func(int) (string, error)
	Terminate        func(int) error
	Probe            func() bool
}

// Ensure starts a managed Ollama server when no server is listening locally.
func (manager Manager) Ensure(ctx context.Context, binary string, settings config.Config) error {
	probe := manager.Probe
	if probe == nil {
		probe = reachable
	}
	if probe() {
		state, owned := manager.recordedServer()
		if owned && state.Matches(settings) {
			return nil
		}
		if !owned {
			return fmt.Errorf("Ollama is already listening on 127.0.0.1:11434 outside this application's managed server")
		}
		if err := manager.Stop(); err != nil {
			return fmt.Errorf("restart managed Ollama server: %w", err)
		}
		for probe() {
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for managed Ollama server to stop: %w", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	command := exec.Command(binary, "serve")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Env = append(os.Environ(), "OLLAMA_HOST=127.0.0.1:11434", "OLLAMA_NO_CLOUD=1", "OLLAMA_MODELS="+settings.ModelDirectory, fmt.Sprintf("OLLAMA_CONTEXT_LENGTH=%d", settings.ContextLength))
	log, err := os.OpenFile(manager.StatePath+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Ollama log: %w", err)
	}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		log.Close()
		return fmt.Errorf("start Ollama: %w", err)
	}
	if err := SaveState(manager.StatePath, State{
		PID:            command.Process.Pid,
		ModelDirectory: settings.ModelDirectory,
		ContextLength:  settings.ContextLength,
	}); err != nil {
		command.Process.Kill()
		log.Close()
		return err
	}
	log.Close()
	go command.Wait()
	for {
		if probe() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (manager Manager) ownsRecordedServer() bool {
	_, owned := manager.recordedServer()
	return owned
}

func (manager Manager) recordedServer() (State, bool) {
	data, err := os.ReadFile(manager.StatePath)
	if err != nil {
		return State{}, false
	}
	var state State
	if json.Unmarshal(data, &state) != nil || state.PID <= 0 {
		return State{}, false
	}
	arguments := manager.ProcessArguments
	if arguments == nil {
		arguments = processArguments
	}
	command, err := arguments(state.PID)
	return state, err == nil && strings.Contains(command, "ollama serve")
}

// SaveState atomically records a managed process identity.
func SaveState(path string, state State) error {
	if state.PID <= 0 {
		return fmt.Errorf("managed Ollama PID must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create server state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode server state: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Stop stops a process only when its saved identity still identifies Ollama serve.
func (manager Manager) Stop() error {
	data, err := os.ReadFile(manager.StatePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("no managed Ollama server is running")
	}
	if err != nil {
		return fmt.Errorf("read managed server state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil || state.PID <= 0 {
		return fmt.Errorf("managed server state is invalid")
	}
	arguments := manager.ProcessArguments
	if arguments == nil {
		arguments = processArguments
	}
	command, err := arguments(state.PID)
	if err != nil || !strings.Contains(command, "ollama serve") {
		return fmt.Errorf("refusing to stop unverified managed Ollama process %d", state.PID)
	}
	stopper := manager.Terminate
	if stopper == nil {
		stopper = terminate
	}
	if err := stopper(state.PID); err != nil {
		return fmt.Errorf("stop managed Ollama process: %w", err)
	}
	if err := os.Remove(manager.StatePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear managed server state: %w", err)
	}
	return nil
}

// StopAndWait stops a managed server and waits until its listener has closed.
func (manager Manager) StopAndWait(ctx context.Context) (bool, error) {
	if err := manager.Stop(); err != nil {
		if strings.Contains(err.Error(), "no managed Ollama server is running") {
			return false, nil
		}
		return false, err
	}
	probe := manager.Probe
	if probe == nil {
		probe = reachable
	}
	for probe() {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("wait for managed Ollama server to stop: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	return true, nil
}

func processArguments(pid int) (string, error) {
	output, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "args=").Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

func reachable() bool {
	connection, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return false
	}
	defer syscall.Close(connection)
	address := &syscall.SockaddrInet4{Port: 11434, Addr: [4]byte{127, 0, 0, 1}}
	return syscall.Connect(connection, address) == nil
}

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
)

func TestStopRefusesUnknownOrStaleProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := SaveState(path, State{PID: 42}); err != nil {
		t.Fatal(err)
	}
	manager := Manager{
		StatePath:        path,
		ProcessArguments: func(int) (string, error) { return "unrelated process", nil },
		Terminate:        func(int) error { t.Fatal("Terminate called"); return nil },
	}
	if err := manager.Stop(); err == nil {
		t.Fatal("Stop succeeded")
	}
}

func TestStopTerminatesVerifiedOllamaAndClearsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := SaveState(path, State{PID: 42}); err != nil {
		t.Fatal(err)
	}
	terminated := 0
	manager := Manager{
		StatePath:        path,
		ProcessArguments: func(int) (string, error) { return "/usr/bin/ollama serve", nil },
		Terminate:        func(pid int) error { terminated = pid; return nil },
	}
	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}
	if terminated != 42 {
		t.Fatalf("terminated = %d", terminated)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state still exists: %v", err)
	}
}

func TestStopAndWaitStopsManagedServerBeforeStorageChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := SaveState(path, State{PID: 42}); err != nil {
		t.Fatal(err)
	}
	running := true
	manager := Manager{
		StatePath:        path,
		ProcessArguments: func(int) (string, error) { return "/usr/bin/ollama serve", nil },
		Terminate:        func(int) error { running = false; return nil },
		Probe:            func() bool { return running },
	}
	stopped, err := manager.StopAndWait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("storage reconfiguration must report that it stopped the managed server")
	}
}

func TestEnsureRefusesExternalListener(t *testing.T) {
	manager := Manager{
		StatePath: filepath.Join(t.TempDir(), "server.json"),
		Probe:     func() bool { return true },
	}
	err := manager.Ensure(t.Context(), "ollama", config.Config{ModelDirectory: "/models", ContextLength: 16384})
	if err == nil || err.Error() != "Ollama is already listening on 127.0.0.1:11434 outside this application's managed server" {
		t.Fatalf("Ensure() error = %v", err)
	}
}

func TestStateRequiresRestartWhenStorageOrContextChanges(t *testing.T) {
	state := State{PID: 42, ModelDirectory: "/models/old", ContextLength: 16384}
	if (State{PID: 42}).Matches(config.Config{ModelDirectory: "/models/old", ContextLength: 16384}) {
		t.Fatal("legacy state without storage metadata must restart the managed Ollama server")
	}
	if !state.Matches(config.Config{ModelDirectory: "/models/old", ContextLength: 16384}) {
		t.Fatal("state must match its original Ollama configuration")
	}
	if state.Matches(config.Config{ModelDirectory: "/models/new", ContextLength: 16384}) {
		t.Fatal("changed model storage must restart the managed Ollama server")
	}
	if state.Matches(config.Config{ModelDirectory: "/models/old", ContextLength: 32768}) {
		t.Fatal("changed context length must restart the managed Ollama server")
	}
}

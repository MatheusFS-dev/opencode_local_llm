package server

import (
	"os"
	"path/filepath"
	"testing"
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

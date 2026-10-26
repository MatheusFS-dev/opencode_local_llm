package worker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArgumentsUsesSelectedOllamaModel(t *testing.T) {
	arguments, err := BuildArguments(Request{
		Project:  "/project with spaces",
		Task:     "Add status command",
		Criteria: "Run shell tests",
		Model:    "huihui_ai/qwen3.5-abliterated:4B",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--standalone", "--agent", "build", "--model", "ollama/huihui_ai/qwen3.5-abliterated:4B"}
	if strings.Join(arguments[:len(want)], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("arguments = %#v", arguments)
	}
	if !strings.Contains(arguments[len(arguments)-1], "Implement: Add status command") || !strings.Contains(arguments[len(arguments)-1], "Acceptance criteria: Run shell tests") || !strings.Contains(arguments[len(arguments)-1], "Do not commit") {
		t.Fatalf("prompt = %q", arguments[len(arguments)-1])
	}
}

func TestRunPreservesProjectPathAndChildOutput(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'dir=%s\\n' \"$PWD\"\nprintf 'args=%s\\n' \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(directory, "project with spaces")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Run(context.Background(), binary, Request{Project: project, Task: "Task", Criteria: "Criteria", Model: "model:4b"}, &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "dir="+project) || !strings.Contains(output.String(), "--model ollama/model:4b") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestBuildArgumentsRejectsMissingRequiredValues(t *testing.T) {
	_, err := BuildArguments(Request{Project: t.TempDir(), Task: "", Criteria: "criteria", Model: "model"})
	if err == nil {
		t.Fatal("BuildArguments succeeded")
	}
}

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("TB_WORKER_TEST_PRINT_PWD") == "1" {
		fmt.Fprint(os.Stdout, os.Getenv("PWD"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestReadOnlyVerificationCannotImplementTheDeliverable(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("Bubblewrap is required for supervised verification")
	}
	project := t.TempDir()
	binary := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Run(t.Context(), binary, Request{Project: project, Task: "Create deliverable", Criteria: "File exists", Model: "local:4b", ContextLength: 16384, ReadOnlyVerification: true, Verify: "printf wrong > deliverable.sh"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "acceptance check failed") {
		t.Fatalf("write was accepted: %v %s", err, output.String())
	}
	if _, err := os.Stat(filepath.Join(project, "deliverable.sh")); !os.IsNotExist(err) {
		t.Fatal("controller acceptance command implemented the task")
	}
}

func TestDeclaredArtifactIsRequiredEvenWithVacuousShellCheck(t *testing.T) {
	project := t.TempDir()
	binary := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Run(t.Context(), binary, Request{Project: project, Task: "Create deliverable", Criteria: "File exists", Model: "local:4b", ContextLength: 16384, Verify: "test -d .", Artifacts: []Artifact{{Path: filepath.Join(project, "missing.sh"), Executable: true}}}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "declared artifact is missing") {
		t.Fatalf("missing artifact was accepted: %v", err)
	}
	if !strings.Contains(err.Error(), "does not establish a permission or read-only filesystem problem") {
		t.Fatalf("missing artifact error must distinguish absent output from permissions: %v", err)
	}
}

func TestArtifactScopeRequiresExplicitExternalAuthorization(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(outside, "existing.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifacts([]Artifact{{Path: path}}, []string{project}); err == nil {
		t.Fatal("unrelated existing host file accepted")
	}
	if err := os.Symlink(outside, filepath.Join(project, "link")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArtifacts([]Artifact{{Path: filepath.Join(project, "link", "existing.sh")}}, []string{project}); err == nil {
		t.Fatal("symlink escaped artifact scope")
	}
	if err := ValidateArtifacts([]Artifact{{Path: path}}, []string{project, outside}); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactChecksUseTheAuthorizedExecutableFile(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, "check.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkArtifact(Artifact{Path: path, Executable: true}, []string{project}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkArtifact(Artifact{Path: path, Executable: true}, []string{project}); err == nil {
		t.Fatal("non-executable file accepted")
	}
}

func TestRunAlignsWorkingDirectoryEnvironment(t *testing.T) {
	project := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TB_WORKER_TEST_PRINT_PWD", "1")
	t.Setenv("PWD", "/wrong-launch-directory")
	var output bytes.Buffer
	if err := Run(t.Context(), binary, Request{Project: project, Task: "Task", Criteria: "Criteria", Model: "local:4b", Verify: "true", ContextLength: 16384}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != project {
		t.Fatalf("child PWD = %q, want %q", output.String(), project)
	}
}

func TestBuildArgumentsUsesSelectedOllamaModel(t *testing.T) {
	arguments, err := BuildArguments(Request{
		Project:       "/project with spaces",
		Task:          "Add status command",
		Criteria:      "Run shell tests",
		Model:         "huihui_ai/qwen3.5-abliterated:4B",
		Verify:        "test -f status.sh",
		ContextLength: 16384,
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
	err := Run(context.Background(), binary, Request{Project: project, Task: "Task", Criteria: "Criteria", Model: "model:4b", Verify: "true", ContextLength: 16384}, &output, &output)
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

func TestWorkerBlocksBroadProcessTermination(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"policies":[{"action":"tool.use","resource":"shell:git push *","effect":"deny"}]}`)
	raw, err := runtimeConfig(Request{Model: "local:4b", ContextLength: 32768})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Policies []struct {
			Action   string
			Resource string
			Effect   string
		}
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"shell:git push *", "shell:*pkill*", "shell:*killall*"} {
		found := false
		for _, rule := range config.Policies {
			if rule.Action == "tool.use" && rule.Resource == resource && rule.Effect == "deny" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing enforced policy %s", resource)
		}
	}
}

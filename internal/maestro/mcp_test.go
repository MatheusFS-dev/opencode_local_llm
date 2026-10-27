package maestro

import (
	"bytes"
	"encoding/json"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerMCPRejectsFailedJobReview(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job, err := store.Create(worker.Request{Project: t.TempDir(), Task: "create script", Criteria: "executable script", Verify: "test -f cat.sh", Model: "local:4b", ContextLength: 16384}, config.Config{}, "interactive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	job.Status, job.Error = Failed, "worker exit 7"
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	server := WorkerServer{Store: store, Root: job.Request.Project}
	args, _ := json.Marshal(map[string]any{"job_id": job.ID, "approved": true, "reason": "looks fine"})
	if _, err := server.Call("review_job", args); err == nil {
		t.Fatal("accepted failed worker")
	}
	actual, _ := store.Load(job.ID)
	if actual.Status != Failed {
		t.Fatal("overwrote failure")
	}
}

func TestWorkerMCPLogsIncludeFinalOutput(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job, err := store.Create(worker.Request{Project: t.TempDir(), Task: "create script", Criteria: "works", Verify: "test -f cat.sh", Model: "local:4b", ContextLength: 16384}, config.Config{}, "interactive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	directory, _ := store.Directory(job.ID)
	if err := os.WriteFile(filepath.Join(directory, "worker.log"), []byte(strings.Repeat("x", 70000)+"FINAL TEST RESULT"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"job_id": job.ID})
	result, err := (WorkerServer{Store: store, Root: job.Request.Project}).Call("job_logs", args)
	if err != nil || !strings.HasSuffix(result, "FINAL TEST RESULT") || len(result) > 66000 {
		t.Fatalf("missing bounded final result: %d %v", len(result), err)
	}
}

func TestWorkerMCPRejectsOtherWorkspaceJobs(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job, err := store.Create(worker.Request{Project: t.TempDir(), Task: "create script", Criteria: "works", Verify: "test -f cat.sh", Model: "local:4b", ContextLength: 16384}, config.Config{}, "interactive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"job_id": job.ID})
	if _, err := (WorkerServer{Store: store, Root: t.TempDir()}).Call("job_status", args); err == nil {
		t.Fatal("accessed other workspace job")
	}
}

func TestWorkerMCPRejectsStartingOutsideWorkingDirectory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{outside, link} {
		server := WorkerServer{Root: root, Store: Store{Root: t.TempDir()}, Settings: func() (config.Config, error) {
			t.Fatal("loaded settings for unauthorized job")
			return config.Config{}, nil
		}}
		args, _ := json.Marshal(map[string]any{"project": project, "task": "write file", "criteria": "exists", "verify": "test -f cat.sh", "artifacts": []worker.Artifact{{Path: project + "/cat.sh"}}})
		if _, err := server.Call("start_implementation", args); err == nil {
			t.Fatal("started outside session scope")
		}
	}
}

func TestWorkerMCPReviewStartsOnlyThreeRounds(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job, err := store.Create(worker.Request{Project: t.TempDir(), Task: "create script", Criteria: "works", Verify: "test -f cat.sh", Model: "local:4b", ContextLength: 16384}, config.Config{}, "interactive", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := WorkerServer{Store: store, Root: job.Request.Project, Start: func(job Job) error { return nil }}
	for round := 1; round <= 3; round++ {
		job.Status = AwaitingReview
		if err := store.Save(job); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]any{"job_id": job.ID, "approved": false, "reason": "missing behavior", "correction": "fix behavior"})
		result, err := server.Call("review_job", args)
		if round == 3 {
			if err == nil {
				t.Fatal("started fourth round")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var next Job
		if err := json.Unmarshal([]byte(result), &next); err != nil {
			t.Fatal(err)
		}
		if next.Attempt != round+1 || next.ParentID != job.ID {
			t.Fatalf("invalid correction %+v", next)
		}
		job = next
	}
}

func TestWorkerMCPProtocolAdvertisesDelegationWithoutShell(t *testing.T) {
	server := WorkerServer{Store: Store{Root: t.TempDir()}}
	var output bytes.Buffer
	if err := (ContextReader{Roots: []string{t.TempDir()}, Worker: &server}).Serve(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n"), &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range response.Result.Tools {
		names[tool.Name] = true
	}
	if len(names) != 8 || !names["start_implementation"] || !names["wait_for_job"] || names["exec"] {
		t.Fatalf("wrong tool capabilities: %v", names)
	}
}

func TestWorkerMCPStartsPersistentJobWithoutProviderOrBubblewrap(t *testing.T) {
	root := t.TempDir()
	server := WorkerServer{Root: root, Store: Store{Root: t.TempDir()}, Settings: func() (config.Config, error) {
		return config.Config{SelectedModel: "local:4b", ContextLength: 16384}, nil
	}, Start: func(job Job) error { return nil }}
	args, _ := json.Marshal(map[string]any{"project": root, "task": "create cat.sh", "criteria": "dancing cat", "verify": "test -f cat.sh", "artifacts": []worker.Artifact{{Path: root + "/cat.sh", Executable: true}}})
	result, err := server.Call("start_implementation", args)
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal([]byte(result), &job); err != nil {
		t.Fatal(err)
	}
	saved, err := server.Store.Load(job.ID)
	if err != nil || saved.Status != Queued || saved.Request.ReadOnlyVerification || saved.Request.Model != "local:4b" {
		t.Fatalf("invalid persistent native job: %+v %v", saved, err)
	}
}

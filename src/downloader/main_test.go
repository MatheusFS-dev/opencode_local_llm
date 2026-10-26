package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActivateModelPassesSelectedNameAndBase(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "activate.sh")
	output := filepath.Join(dir, "args")
	content := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + output + "\"\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	if err := activateModel(context.Background(), script, "/base path", "team/model:4B", "/state"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "activate\n/base path\nteam/model:4B\n/state\n" {
		t.Fatalf("args=%q err=%v", data, err)
	}
}

func TestSelectionActivatesStoreBeforePull(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "activated")
	script := filepath.Join(dir, "activate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(marker); err != nil {
			t.Error("pull began before activation")
		}
		w.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer server.Close()
	a := app{models: []modelEntry{{Name: "one"}}, width: 80, events: make(chan update, 2), baseURL: server.URL, baseDir: dir, serviceScript: script, stateFile: filepath.Join(dir, "state")}
	selected, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	activated, cmd := selected.Update(cmd())
	finished, _ := activated.Update(cmd())
	if result := finished.(app); result.active || result.status.Status != "success" || result.failure != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestActivationFailureExitsBeforePull(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "activate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'service unavailable' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("pull ran after activation failed")
	}))
	defer server.Close()
	a := app{models: []modelEntry{{Name: "one"}}, width: 80, events: make(chan update, 2), baseURL: server.URL, baseDir: dir, serviceScript: script, stateFile: filepath.Join(dir, "state")}
	selected, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	failed, _ := selected.Update(cmd())
	result := failed.(app)
	if result.failure == nil || !strings.Contains(result.failure.Error(), "exit status 7") || !strings.Contains(result.failure.Error(), "service unavailable") {
		t.Fatalf("failure=%v", result.failure)
	}
}

func TestStatusShowsEncodedModelDirectory(t *testing.T) {
	a := app{models: []modelEntry{{Name: "team/model:4B"}}, width: 160, baseDir: "/data/models"}
	if view := a.View(); !strings.Contains(view, "/data/models/team/model-4B") {
		t.Fatal(view)
	}
}

func TestViewShowsGuideVRAMRequirementsForSelectedModel(t *testing.T) {
	tests := map[string][]string{
		"huihui_ai/gemma-4-abliterated:12b": {"VRAM: 12 GB, 16–32K"},
		"gemma4:12b":                        {"VRAM: 12 GB, 16–32K"},
		"huihui_ai/qwen3.5-abliterated:9b":  {"VRAM: 12 GB, 16–32K", "VRAM: 8 GB, 4–8K tight fit"},
		"qwen3.5:9b":                        {"VRAM: 12 GB, 16–32K", "VRAM: 8 GB, 4–8K tight fit"},
		"huihui_ai/qwen3.5-abliterated:4B":  {"VRAM: 8 GB, 16–32K"},
		"qwen3.5:4b":                        {"VRAM: 8 GB, 16–32K"},
	}
	for model, recommendations := range tests {
		t.Run(model, func(t *testing.T) {
			a := app{models: []modelEntry{{Name: model}}, width: 160, baseDir: "/data/models"}
			view := a.View()
			for _, recommendation := range recommendations {
				if !strings.Contains(view, recommendation) {
					t.Errorf("view missing recommendation %q:\n%s", recommendation, view)
				}
			}
		})
	}
}

func TestCatalog(t *testing.T) {
	for _, content := range []string{`[{"name":"one"}]`, `[{"name":"one"},{"name":"two"}]`} {
		path := filepath.Join(t.TempDir(), "models.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		models, err := loadModels(path)
		if err != nil || len(models) != strings.Count(content, `"name"`) {
			t.Fatalf("models=%v err=%v", models, err)
		}
	}
}

func TestDefaultCatalogIncludesRecommendedModels(t *testing.T) {
	models, err := loadModels("../models.json")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"huihui_ai/gemma-4-abliterated:12b": "7.6 GB",
		"gemma4:12b":                        "7.7–8.0 GB",
		"huihui_ai/qwen3.5-abliterated:9b":  "6.6 GB",
		"qwen3.5:9b":                        "6.6–7.6 GB",
		"qwen3.5:4b":                        "3.3–4.0 GB",
	}
	got := make(map[string]string, len(models))
	for _, model := range models {
		got[model.Name] = model.Size
	}
	for name, size := range want {
		if got[name] != size {
			t.Errorf("catalog entry %q = %q, want %q", name, got[name], size)
		}
	}
}

func TestPullStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pull" {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte("{\"status\":\"pulling\",\"total\":100,\"completed\":50}\n{\"status\":\"success\"}\n"))
	}))
	defer server.Close()
	var events []update
	pull(context.Background(), server.Client(), server.URL, "one", func(u update) { events = append(events, u) })
	if len(events) != 2 || events[0].progress.Completed != 50 || !events[1].done {
		t.Fatalf("events=%+v", events)
	}
}
func TestPullError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"error":"missing"}`, 404) }))
	defer server.Close()
	var result update
	pull(context.Background(), server.Client(), server.URL, "one", func(u update) { result = u })
	if result.err == nil || !strings.Contains(result.err.Error(), "HTTP 404") || !strings.Contains(result.err.Error(), "missing") {
		t.Fatal(result.err)
	}
}
func TestPullCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var result update
	pull(ctx, http.DefaultClient, "http://127.0.0.1:1", "one", func(u update) { result = u })
	if result.err == nil {
		t.Fatal("expected cancellation error")
	}
}

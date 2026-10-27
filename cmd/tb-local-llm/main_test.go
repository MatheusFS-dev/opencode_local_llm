package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestVerifyDownloadRejectsMissingAPIModelOrStorage(t *testing.T) {
	for _, tags := range []string{`{"models":[]}`, `{"models":[{"name":"local:4b"}]}`} {
		t.Run(tags, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tags)) }))
			defer server.Close()
			if err := verifyDownload(ollamaClientFor(server.URL), t.TempDir(), "local:4b"); err == nil {
				t.Fatal("unverified download reported success")
			}
		})
	}
}

func TestVerifyDownloadAcceptsInstalledModelWithCompleteStorage(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifests", "registry.ollama.ai", "library", "local", "4b")
	if err := os.MkdirAll(filepath.Dir(manifest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "blobs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"config":{"digest":"sha256:abc","size":2},"layers":[{"digest":"sha256:def","size":3}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for digest, data := range map[string]string{"abc": "{}", "def": "abc"} {
		if err := os.WriteFile(filepath.Join(root, "blobs", "sha256-"+digest), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"local:4b"}]}`))
	}))
	defer server.Close()
	if err := verifyDownload(ollamaClientFor(server.URL), root, "local:4b"); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(root, "blobs", "sha256-def"), 1); err != nil {
		t.Fatal(err)
	}
	if err := verifyDownload(ollamaClientFor(server.URL), root, "local:4b"); err == nil {
		t.Fatal("truncated blob accepted")
	}
}

func TestFormatGPUStatusUsesReadableUtilizationAndVRAM(t *testing.T) {
	got := formatGPUStatus("NVIDIA GeForce RTX 3060, 0, 763, 12288\nNVIDIA GeForce RTX 4070, 5, 12282, 12282")
	want := "GPU: NVIDIA GeForce RTX 3060\n0% utilization, 763 MiB / 12,288 MiB VRAM used\n\nGPU: NVIDIA GeForce RTX 4070\n5% utilization, 12,282 MiB / 12,282 MiB VRAM used"
	if got != want {
		t.Fatalf("formatGPUStatus() = %q, want %q", got, want)
	}
}

func TestFormatGPUStatusRejectsMalformedRows(t *testing.T) {
	if got := formatGPUStatus("NVIDIA GPU, unavailable"); got != "Unavailable" {
		t.Fatalf("formatGPUStatus() = %q, want Unavailable", got)
	}
}

func TestOllamaClientForAllowsLongRunningPullStreams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/pull" {
			t.Fatalf("path = %q, want /api/pull", request.URL.Path)
		}
		time.Sleep(25 * time.Millisecond)
		_, _ = writer.Write([]byte("{\"status\":\"success\"}\n"))
	}))
	defer server.Close()

	if err := ollamaClientFor(server.URL).Pull(context.Background(), "local:4b", nil); err != nil {
		t.Fatalf("Pull() = %v, want long-running stream to complete", err)
	}
}

func TestOpenCodeSessionCommandSetsTheInteractiveSessionModelThroughConfig(t *testing.T) {
	command := openCodeSessionCommand("/usr/local/bin/opencode", "/workspace/project", "huihui_ai/qwen3.5-abliterated:9b")

	if got, want := command.Args, []string{"/usr/local/bin/opencode"}; !slices.Equal(got, want) {
		t.Fatalf("Args = %q, want %q", got, want)
	}
	if got, want := command.Dir, "/workspace/project"; got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
	if !slices.Contains(command.Env, `OPENCODE_CONFIG_CONTENT={"model":"ollama/huihui_ai/qwen3.5-abliterated:9b"}`) {
		t.Fatalf("interactive session environment does not set the selected Ollama model: %q", command.Env)
	}
}

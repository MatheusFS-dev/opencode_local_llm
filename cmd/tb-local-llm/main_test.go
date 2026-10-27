package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

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

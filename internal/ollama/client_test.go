package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListsLoadedAndInstalledModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/ps":
			writer.Write([]byte(`{"models":[{"name":"local:4b","size_vram":42}]}`))
		case "/api/tags":
			writer.Write([]byte(`{"models":[{"name":"local:4b","size":84}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := New(server.URL, server.Client())
	loaded, err := client.Loaded(context.Background())
	if err != nil || len(loaded) != 1 || loaded[0].VRAMBytes != 42 {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
	installed, err := client.Installed(context.Background())
	if err != nil || len(installed) != 1 || installed[0].SizeBytes != 84 {
		t.Fatalf("installed = %#v, err = %v", installed, err)
	}
}

func TestClientDeletesByAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/api/delete" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := New(server.URL, server.Client()).Delete(context.Background(), "local:4b"); err != nil {
		t.Fatal(err)
	}
}

func TestClientPullReportsStreamingProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/pull" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "local:4b" || body["stream"] != true {
			t.Fatalf("body = %#v", body)
		}
		_, _ = writer.Write([]byte("{\"status\":\"pulling\",\"completed\":4,\"total\":8}\n{\"status\":\"success\"}\n"))
	}))
	defer server.Close()
	var progress []Progress
	if err := New(server.URL, server.Client()).Pull(context.Background(), "local:4b", func(value Progress) {
		progress = append(progress, value)
	}); err != nil {
		t.Fatal(err)
	}
	if len(progress) != 2 || progress[0].Completed != 4 || !progress[1].Complete {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestClientPullRejectsStreamWithoutTerminalSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("{\"status\":\"pulling\"}\n"))
	}))
	defer server.Close()
	if err := New(server.URL, server.Client()).Pull(context.Background(), "local:4b", nil); err == nil {
		t.Fatal("Pull succeeded without terminal success")
	}
}

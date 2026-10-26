package ollama

import (
	"context"
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

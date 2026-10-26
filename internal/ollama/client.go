package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Model describes an Ollama model installed or loaded in memory.
type Model struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size"`
	VRAMBytes int64  `json:"size_vram"`
}

// Client communicates with a local Ollama server.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates an Ollama client for one server base URL.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// Loaded returns models currently resident in Ollama memory.
func (client *Client) Loaded(ctx context.Context) ([]Model, error) {
	var response struct {
		Models []Model `json:"models"`
	}
	if err := client.get(ctx, "/api/ps", &response); err != nil {
		return nil, err
	}
	return response.Models, nil
}

// Installed returns models known to the local Ollama server.
func (client *Client) Installed(ctx context.Context) ([]Model, error) {
	var response struct {
		Models []Model `json:"models"`
	}
	if err := client.get(ctx, "/api/tags", &response); err != nil {
		return nil, err
	}
	return response.Models, nil
}

// Delete removes one named model through Ollama's supported API.
func (client *Client) Delete(ctx context.Context, model string) error {
	body, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		return fmt.Errorf("encode model deletion: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, client.baseURL+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Ollama deletion request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("delete Ollama model: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("delete Ollama model: HTTP %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func (client *Client) get(ctx context.Context, path string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create Ollama request: %w", err)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("request Ollama %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("request Ollama %s: HTTP %s: %s", path, response.Status, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("decode Ollama %s response: %w", path, err)
	}
	return nil
}

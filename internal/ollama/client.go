package ollama

import (
	"bufio"
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

// Progress reports one status record from a streaming model pull.
type Progress struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Complete  bool   `json:"-"`
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

// Healthy reports whether the Ollama API answers its version endpoint.
func (client *Client) Healthy(ctx context.Context) error {
	var response struct {
		Version string `json:"version"`
	}
	return client.get(ctx, "/api/version", &response)
}

// Pull downloads a catalog model and forwards each streamed progress record.
func (client *Client) Pull(ctx context.Context, model string, report func(Progress)) error {
	body, err := json.Marshal(map[string]any{"model": model, "stream": true})
	if err != nil {
		return fmt.Errorf("encode model pull: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Ollama pull request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("pull Ollama model: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("pull Ollama model: HTTP %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	complete := false
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		var progress Progress
		if err := json.Unmarshal(scanner.Bytes(), &progress); err != nil {
			return fmt.Errorf("decode Ollama pull response: %w", err)
		}
		progress.Complete = progress.Status == "success"
		if report != nil {
			report(progress)
		}
		complete = complete || progress.Complete
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Ollama pull response: %w", err)
	}
	if !complete {
		return fmt.Errorf("Ollama pull ended without a success response")
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

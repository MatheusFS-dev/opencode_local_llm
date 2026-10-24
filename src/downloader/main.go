package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type modelEntry struct {
	Name string `json:"name"`
	Size string `json:"size"`
}
type progress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}
type update struct {
	progress progress
	err      error
	done     bool
}
type activationResult struct{ err error }
type app struct {
	models                  []modelEntry
	selected, width, height int
	status                  progress
	active                  bool
	activating              bool
	events                  chan update
	ctx                     context.Context
	cancel                  context.CancelFunc
	failure                 error
	baseURL                 string
	baseDir                 string
	serviceScript           string
	stateFile               string
	mode                    string
}

func activateModel(ctx context.Context, script, base, model, state string) error {
	cmd := exec.CommandContext(ctx, script, "activate", base, model, state)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s activate %q %q %q: %w: %s", script, base, model, state, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func loadModels(path string) ([]modelEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var models []modelEntry
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("model catalog is empty")
	}
	for _, model := range models {
		if model.Name == "" {
			return nil, fmt.Errorf("model name is empty")
		}
	}
	return models, nil
}

func pull(ctx context.Context, client *http.Client, baseURL, model string, send func(update)) {
	endpoint := baseURL + "/api/pull"
	body, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		send(update{err: err, done: true})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		send(update{err: fmt.Errorf("POST %s: %w", endpoint, err), done: true})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		send(update{err: fmt.Errorf("POST %s: HTTP %s: %s", endpoint, resp.Status, strings.TrimSpace(string(data))), done: true})
		return
	}
	decoder := json.NewDecoder(resp.Body)
	for {
		var p progress
		err := decoder.Decode(&p)
		if err == io.EOF {
			send(update{err: fmt.Errorf("POST %s: stream ended without success", endpoint), done: true})
			return
		}
		if err != nil {
			send(update{err: fmt.Errorf("POST %s: %w", endpoint, err), done: true})
			return
		}
		if p.Error != "" {
			send(update{err: fmt.Errorf("POST %s: %s", endpoint, p.Error), done: true})
			return
		}
		send(update{progress: p, done: p.Status == "success"})
		if p.Status == "success" {
			return
		}
	}
}
func waitEvent(ch <-chan update) tea.Cmd { return func() tea.Msg { return <-ch } }
func (a app) Init() tea.Cmd              { return nil }
func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
	case tea.KeyMsg:
		switch m.String() {
		case "q", "ctrl+c", "esc":
			if a.activating {
				return a, nil
			}
			if a.cancel != nil {
				a.cancel()
			}
			return a, tea.Quit
		case "up", "k":
			if !a.active && a.selected > 0 {
				a.selected--
			}
		case "down", "j":
			if !a.active && a.selected < len(a.models)-1 {
				a.selected++
			}
		case "enter":
			if !a.active && a.width >= 80 {
				ctx, cancel := context.WithCancel(context.Background())
				a.ctx, a.cancel = ctx, cancel
				a.active = true
				a.activating = true
				a.status = progress{Status: "Configuring Ollama"}
				model := a.models[a.selected].Name
				if a.mode == "persistent" {
					cmd := exec.CommandContext(ctx, a.serviceScript, "activate", a.baseDir, model, a.stateFile)
					return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
						if err != nil {
							err = fmt.Errorf("%s activate %q %q %q: %w", a.serviceScript, a.baseDir, model, a.stateFile, err)
						}
						return activationResult{err}
					})
				}
				return a, func() tea.Msg {
					return activationResult{activateModel(ctx, a.serviceScript, a.baseDir, model, a.stateFile)}
				}
			}
		}
	case activationResult:
		if m.err != nil {
			a.failure = m.err
			a.cancel()
			return a, tea.Quit
		}
		a.activating = false
		a.status = progress{Status: "Connecting"}
		go pull(a.ctx, http.DefaultClient, a.baseURL, a.models[a.selected].Name, func(u update) {
			select {
			case a.events <- u:
			case <-a.ctx.Done():
			}
		})
		return a, waitEvent(a.events)
	case update:
		if m.err != nil {
			a.failure = m.err
			a.cancel()
			return a, tea.Quit
		}
		a.status = m.progress
		if m.done {
			a.active = false
			a.cancel()
			a.cancel = nil
			return a, nil
		}
		return a, waitEvent(a.events)
	}
	return a, nil
}
func (a app) View() string {
	if a.width < 80 {
		return "Resize terminal to at least 80 columns.\n"
	}
	leftWidth := a.width / 2
	left := []string{"Models", ""}
	for i, model := range a.models {
		marker := "  "
		if i == a.selected {
			marker = "> "
		}
		left = append(left, marker+model.Name+" ("+model.Size+")")
	}
	right := []string{"Status", "", "Model: " + a.models[a.selected].Name, "Status: " + a.status.Status, "Storage: " + strings.TrimRight(a.baseDir, "/") + "/" + strings.ReplaceAll(a.models[a.selected].Name, ":", "-")}
	if a.status.Total > 0 {
		right = append(right, fmt.Sprintf("Progress: %.1f%% (%d / %d bytes)", 100*float64(a.status.Completed)/float64(a.status.Total), a.status.Completed, a.status.Total))
	}
	if a.status.Digest != "" {
		right = append(right, "Layer: "+a.status.Digest)
	}
	var out strings.Builder
	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if len(l) > leftWidth-2 {
			l = l[:leftWidth-2]
		}
		if len(r) > a.width-leftWidth-2 {
			r = r[:a.width-leftWidth-2]
		}
		fmt.Fprintf(&out, "%-*s│ %s\n", leftWidth, l, r)
	}
	out.WriteString("\n↑/↓ select  Enter pull  q quit\n")
	return out.String()
}
func main() {
	if len(os.Args) != 6 {
		fmt.Fprintln(os.Stderr, "Usage: local-downloader CATALOG BASE_DIR SERVICE_SCRIPT STATE_FILE MODE")
		os.Exit(2)
	}
	models, err := loadModels(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a := app{models: models, events: make(chan update, 64), baseURL: "http://127.0.0.1:11434", baseDir: os.Args[2], serviceScript: os.Args[3], stateFile: os.Args[4], mode: os.Args[5]}
	result, err := tea.NewProgram(a, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if failure := result.(app).failure; failure != nil {
		fmt.Fprintln(os.Stderr, failure)
		os.Exit(1)
	}
}

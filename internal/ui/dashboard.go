package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
)

// Snapshot contains live information rendered by the dashboard.
type Snapshot struct {
	Server     string
	GPU        string
	LoadedVRAM string
	Installed  map[string]bool
}

// Actions provides dashboard operations without coupling its view to system APIs.
type Actions struct {
	Setup    func() error
	Start    func() error
	Stop     func() error
	Download func(string) error
	Delete   func(string) error
	Select   func(string) error
	Launch   func() (*exec.Cmd, error)
	Refresh  func() Snapshot
}

type operationMsg struct {
	name string
	err  error
}

type refreshMsg Snapshot
type launchFinishedMsg struct{ err error }

// Dashboard presents local model setup and runtime state in the terminal.
type Dashboard struct {
	config        config.Config
	models        []models.Entry
	actions       Actions
	snapshot      Snapshot
	activity      string
	selected      int
	confirmDelete bool
	width         int
	height        int
}

// New creates a dashboard with persisted configuration and optional operations.
func New(saved config.Config, catalog []models.Entry, actions ...Actions) *Dashboard {
	dashboard := &Dashboard{config: saved, models: catalog, width: 80, height: 24, activity: "Ready"}
	if len(actions) > 0 {
		dashboard.actions = actions[0]
	}
	return dashboard
}

// Resize updates dashboard dimensions.
func (dashboard *Dashboard) Resize(width, height int) {
	dashboard.width = max(20, width)
	dashboard.height = max(8, height)
}

// Init begins periodic non-blocking status refreshes.
func (dashboard *Dashboard) Init() tea.Cmd { return dashboard.refresh() }

// Update handles dashboard keyboard commands and asynchronous action results.
func (dashboard *Dashboard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		dashboard.Resize(value.Width, value.Height)
	case refreshMsg:
		dashboard.snapshot = Snapshot(value)
		return dashboard, tea.Tick(time.Second, func(time.Time) tea.Msg { return refreshMsg(dashboard.readSnapshot()) })
	case operationMsg:
		if value.err != nil {
			dashboard.activity = fmt.Sprintf("%s failed: %v", value.name, value.err)
		} else {
			dashboard.activity = value.name + " completed"
		}
		return dashboard, dashboard.refresh()
	case launchFinishedMsg:
		if value.err != nil {
			dashboard.activity = "OpenCode failed: " + value.err.Error()
		} else {
			dashboard.activity = "OpenCode session completed"
		}
		return dashboard, dashboard.refresh()
	case tea.KeyPressMsg:
		return dashboard.key(value)
	}
	return dashboard, nil
}

func (dashboard *Dashboard) key(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if dashboard.confirmDelete {
		dashboard.confirmDelete = false
		if value.Code == 'y' {
			return dashboard, dashboard.operation("Delete model", func() error { return dashboard.callDelete(dashboard.current().Name) })
		}
		dashboard.activity = "Delete cancelled"
		return dashboard, nil
	}
	switch value.Code {
	case 'q':
		return dashboard, tea.Quit
	case 'c':
		if value.Mod.Contains(tea.ModCtrl) {
			return dashboard, tea.Quit
		}
	case tea.KeyUp:
		if dashboard.selected > 0 {
			dashboard.selected--
		}
	case tea.KeyDown:
		if dashboard.selected+1 < len(dashboard.models) {
			dashboard.selected++
		}
	case 's':
		return dashboard, dashboard.operation("Set up Ollama", dashboard.actions.Setup)
	case 'r':
		return dashboard, dashboard.operation("Start server", dashboard.actions.Start)
	case 'x':
		return dashboard, dashboard.operation("Stop server", dashboard.actions.Stop)
	case 'd':
		return dashboard, dashboard.operation("Download model", func() error { return dashboard.callDownload(dashboard.current().Name) })
	case 'e', tea.KeyEnter:
		return dashboard, dashboard.operation("Select model", func() error { return dashboard.callSelect(dashboard.current().Name) })
	case tea.KeyDelete:
		if dashboard.current().Name == "" {
			return dashboard, nil
		}
		dashboard.confirmDelete = true
		dashboard.activity = "Delete " + dashboard.current().Name + "? Press y to confirm"
	case 'o':
		if dashboard.actions.Launch == nil {
			dashboard.activity = "OpenCode launch is unavailable"
			return dashboard, nil
		}
		command, err := dashboard.actions.Launch()
		if err != nil {
			dashboard.activity = "OpenCode failed: " + err.Error()
			return dashboard, nil
		}
		dashboard.activity = "Running OpenCode..."
		return dashboard, tea.ExecProcess(command, func(err error) tea.Msg { return launchFinishedMsg{err: err} })
	}
	return dashboard, nil
}

func (dashboard *Dashboard) operation(name string, action func() error) tea.Cmd {
	if action == nil {
		return func() tea.Msg { return operationMsg{name: name, err: fmt.Errorf("this action is unavailable")} }
	}
	return func() tea.Msg { return operationMsg{name: name, err: action()} }
}

func (dashboard *Dashboard) refresh() tea.Cmd {
	return func() tea.Msg { return refreshMsg(dashboard.readSnapshot()) }
}

func (dashboard *Dashboard) readSnapshot() Snapshot {
	if dashboard.actions.Refresh == nil {
		return dashboard.snapshot
	}
	return dashboard.actions.Refresh()
}

func (dashboard *Dashboard) current() models.Entry {
	if dashboard.selected < 0 || dashboard.selected >= len(dashboard.models) {
		return models.Entry{}
	}
	return dashboard.models[dashboard.selected]
}

func (dashboard *Dashboard) callDownload(model string) error {
	if model == "" {
		return fmt.Errorf("select a catalog model first")
	}
	if dashboard.actions.Download == nil {
		return fmt.Errorf("model download is unavailable")
	}
	return dashboard.actions.Download(model)
}

func (dashboard *Dashboard) callDelete(model string) error {
	if model == "" {
		return fmt.Errorf("select an installed model first")
	}
	if dashboard.actions.Delete == nil {
		return fmt.Errorf("model deletion is unavailable")
	}
	return dashboard.actions.Delete(model)
}

func (dashboard *Dashboard) callSelect(model string) error {
	if model == "" {
		return fmt.Errorf("select a catalog model first")
	}
	if !dashboard.snapshot.Installed[model] {
		return fmt.Errorf("%s is not installed; download it first", model)
	}
	if dashboard.actions.Select == nil {
		return fmt.Errorf("model selection is unavailable")
	}
	if err := dashboard.actions.Select(model); err != nil {
		return err
	}
	dashboard.config.SelectedModel = model
	return nil
}

// View renders an alternate-screen local LLM dashboard.
func (dashboard *Dashboard) View() tea.View {
	width := dashboard.width
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Width(width).Align(lipgloss.Center).Render("LOCAL LLM")
	overview := dashboard.panel("OVERVIEW", fmt.Sprintf("Server: %s\nSelected: %s\nContext: %d tokens\nStorage: %s", empty(dashboard.snapshot.Server), empty(dashboard.config.SelectedModel), dashboard.config.ContextLength, dashboard.config.ModelDirectory), dashboard.panelWidth())
	resources := dashboard.panel("RESOURCES", fmt.Sprintf("GPU: %s\nLoaded model VRAM: %s", empty(dashboard.snapshot.GPU), empty(dashboard.snapshot.LoadedVRAM)), dashboard.panelWidth())
	modelNames := make([]string, 0, len(dashboard.models))
	for index, model := range dashboard.models {
		marker := " "
		if index == dashboard.selected {
			marker = ">"
		}
		installed := ""
		if dashboard.snapshot.Installed[model.Name] {
			installed = " installed"
		}
		modelNames = append(modelNames, fmt.Sprintf("%s %s  •  %s%s", marker, model.Name, model.Size, installed))
	}
	modelsPanel := dashboard.panel("MODELS", empty(strings.Join(modelNames, "\n")), dashboard.panelWidth())
	actions := dashboard.panel("ACTIONS", "[S] Setup  [R] Start  [X] Stop\n[D] Download  [Enter] Select  [Del] Remove\n[O] Run OpenCode  [↑↓] Browse  [Q] Quit\n\n"+dashboard.activity, dashboard.panelWidth())
	var body string
	if width >= 86 {
		body = lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, overview, " ", resources), lipgloss.JoinHorizontal(lipgloss.Top, modelsPanel, " ", actions))
	} else {
		body = lipgloss.JoinVertical(lipgloss.Left, overview, resources, modelsPanel, actions)
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(width).Render("All actions use local Ollama • q quit")
	view := tea.NewView(strings.Join([]string{title, "", body, footer}, "\n"))
	view.AltScreen = true
	view.WindowTitle = "Local LLM"
	return view
}

func (dashboard *Dashboard) panelWidth() int {
	if dashboard.width >= 86 {
		return max(20, (dashboard.width-1)/2)
	}
	return dashboard.width
}

func (dashboard *Dashboard) panel(title, body string, width int) string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(title)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1).Width(width).Render(heading + "\n" + body)
}

func empty(value string) string {
	if value == "" {
		return "Unavailable"
	}
	return value
}

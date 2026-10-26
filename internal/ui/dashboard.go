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
	Reset    func() (config.Config, error)
	Download func(string) error
	Delete   func(string) error
	Select   func(string) error
	Launch   func() (*exec.Cmd, error)
	Refresh  func() Snapshot
}

type action int

const (
	setupAction action = iota
	startServerAction
	downloadModelAction
	selectModelAction
	launchOpenCodeAction
	stopServerAction
	deleteModelAction
	resetConfigurationAction
)

var actionLabels = []string{
	"Set up Ollama",
	"Start managed Ollama server",
	"Download a model",
	"Choose default model",
	"Start OpenCode session",
	"Stop managed Ollama server",
	"Delete a model",
	"Reset local configuration",
}

type operationMsg struct {
	name string
	err  error
}

type refreshMsg Snapshot
type launchFinishedMsg struct{ err error }

// Dashboard presents local model setup and runtime state in the terminal.
type Dashboard struct {
	config         config.Config
	models         []models.Entry
	actions        Actions
	snapshot       Snapshot
	activity       string
	actionSelected int
	modelSelected  int
	pickerAction   action
	confirmDelete  bool
	confirmReset   bool
	width          int
	height         int
}

// New creates a dashboard with persisted configuration and optional operations.
func New(saved config.Config, catalog []models.Entry, actions ...Actions) *Dashboard {
	dashboard := &Dashboard{config: saved, models: catalog, width: 80, height: 24, activity: "Ready", pickerAction: -1}
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
	if dashboard.confirmReset {
		dashboard.confirmReset = false
		if value.Code != 'y' {
			dashboard.activity = "Configuration reset cancelled"
			return dashboard, nil
		}
		if dashboard.actions.Reset == nil {
			dashboard.activity = "Configuration reset is unavailable"
			return dashboard, nil
		}
		return dashboard, func() tea.Msg {
			settings, err := dashboard.actions.Reset()
			if err == nil {
				dashboard.config = settings
			}
			return operationMsg{name: "Configuration reset", err: err}
		}
	}
	if dashboard.pickerAction >= 0 {
		return dashboard.pickModel(value)
	}
	switch value.Code {
	case 'q':
		return dashboard, tea.Quit
	case 'c':
		if value.Mod.Contains(tea.ModCtrl) {
			return dashboard, tea.Quit
		}
	case tea.KeyUp:
		if dashboard.actionSelected > 0 {
			dashboard.actionSelected--
		}
	case tea.KeyDown:
		if dashboard.actionSelected+1 < len(actionLabels) {
			dashboard.actionSelected++
		}
	case tea.KeyEnter:
		return dashboard, dashboard.selectAction()
	}
	return dashboard, nil
}

func (dashboard *Dashboard) selectAction() tea.Cmd {
	switch action(dashboard.actionSelected) {
	case setupAction:
		return dashboard.operation("Ollama setup", dashboard.actions.Setup)
	case startServerAction:
		return dashboard.operation("Managed Ollama server started", dashboard.actions.Start)
	case downloadModelAction, selectModelAction, deleteModelAction:
		dashboard.pickerAction = action(dashboard.actionSelected)
		dashboard.modelSelected = 0
		dashboard.activity = "Choose a model and press Enter"
		return nil
	case launchOpenCodeAction:
		return dashboard.launchOpenCode()
	case stopServerAction:
		return dashboard.operation("Managed Ollama server stopped", dashboard.actions.Stop)
	case resetConfigurationAction:
		dashboard.confirmReset = true
		dashboard.activity = "Reset saved configuration and stop the managed server? Press y to confirm"
	}
	return nil
}

func (dashboard *Dashboard) pickModel(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch value.Code {
	case tea.KeyEscape:
		dashboard.pickerAction = -1
		dashboard.activity = "Model selection cancelled"
	case tea.KeyUp:
		if dashboard.modelSelected > 0 {
			dashboard.modelSelected--
		}
	case tea.KeyDown:
		if dashboard.modelSelected+1 < len(dashboard.models) {
			dashboard.modelSelected++
		}
	case tea.KeyEnter:
		model := dashboard.current().Name
		switch dashboard.pickerAction {
		case downloadModelAction:
			dashboard.pickerAction = -1
			return dashboard, dashboard.operation("Model download", func() error { return dashboard.callDownload(model) })
		case selectModelAction:
			dashboard.pickerAction = -1
			return dashboard, dashboard.operation("Default model selected", func() error { return dashboard.callSelect(model) })
		case deleteModelAction:
			if model != "" {
				dashboard.confirmDelete = true
				dashboard.pickerAction = -1
				dashboard.activity = "Delete " + model + "? Press y to confirm"
			}
		}
	}
	return dashboard, nil
}

func (dashboard *Dashboard) launchOpenCode() tea.Cmd {
	if dashboard.actions.Launch == nil {
		dashboard.activity = "OpenCode launch is unavailable"
		return nil
	}
	command, err := dashboard.actions.Launch()
	if err != nil {
		dashboard.activity = "OpenCode failed: " + err.Error()
		return nil
	}
	dashboard.activity = "Running OpenCode..."
	return tea.ExecProcess(command, func(err error) tea.Msg { return launchFinishedMsg{err: err} })
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
	if dashboard.modelSelected < 0 || dashboard.modelSelected >= len(dashboard.models) {
		return models.Entry{}
	}
	return dashboard.models[dashboard.modelSelected]
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
	hints := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(width).Align(lipgloss.Center).Render("↑/↓ Navigate  •  Enter select  •  Esc back  •  q quit")
	overview := dashboard.panel("OVERVIEW", fmt.Sprintf("Server: %s\nSelected: %s\nContext: %d tokens\nStorage: %s", empty(dashboard.snapshot.Server), empty(dashboard.config.SelectedModel), dashboard.config.ContextLength, dashboard.config.ModelDirectory), width)
	resources := dashboard.panel("RESOURCES", fmt.Sprintf("%s\nLoaded model VRAM: %s", empty(dashboard.snapshot.GPU), empty(dashboard.snapshot.LoadedVRAM)), width)
	body := lipgloss.JoinVertical(lipgloss.Left, overview, resources, dashboard.actionList())
	view := tea.NewView(strings.Join([]string{title, hints, "", body}, "\n"))
	view.AltScreen = true
	view.WindowTitle = "Local LLM"
	return view
}

func (dashboard *Dashboard) actionList() string {
	lines := make([]string, 0, len(actionLabels)+3)
	for index, label := range actionLabels {
		line := "  " + label
		if index == dashboard.actionSelected {
			line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Render("› " + label)
		}
		lines = append(lines, line)
	}
	if dashboard.pickerAction >= 0 {
		lines = append(lines, "", dashboard.modelPicker())
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(dashboard.activity))
	return lipgloss.NewStyle().Width(dashboard.width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (dashboard *Dashboard) modelPicker() string {
	purpose := map[action]string{
		downloadModelAction: "Choose a model to download",
		selectModelAction:   "Choose the default model",
		deleteModelAction:   "Choose a model to delete",
	}[dashboard.pickerAction]
	lines := []string{purpose}
	for index, model := range dashboard.models {
		marker := " "
		if index == dashboard.modelSelected {
			marker = "›"
		}
		installed := ""
		if dashboard.snapshot.Installed[model.Name] {
			installed = "  installed"
		}
		lines = append(lines, fmt.Sprintf("%s %s  •  %s%s", marker, model.Name, model.Size, installed))
	}
	return strings.Join(lines, "\n")
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

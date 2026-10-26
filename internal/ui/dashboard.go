package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
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
	Setup      func() error
	Start      func() error
	Stop       func() error
	Reset      func() (config.Config, error)
	SetStorage func(string) (config.Config, error)
	Download   func(string) error
	Delete     func(string) error
	Select     func(string) error
	Launch     func(string) (*exec.Cmd, error)
	Refresh    func() Snapshot
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
	configureStorageAction
)

type actionDefinition struct {
	label   string
	section string
	kind    action
}

var actionDefinitions = []actionDefinition{
	{label: "Set up Ollama", section: "SETUP", kind: setupAction},
	{label: "Start managed Ollama server", section: "SETUP", kind: startServerAction},
	{label: "Configure model storage", section: "MODELS", kind: configureStorageAction},
	{label: "Download a model", section: "MODELS", kind: downloadModelAction},
	{label: "Choose default model", section: "MODELS", kind: selectModelAction},
	{label: "Delete a model", section: "MODELS", kind: deleteModelAction},
	{label: "Start OpenCode session", section: "WORK", kind: launchOpenCodeAction},
	{label: "Stop managed Ollama server", section: "MAINTENANCE", kind: stopServerAction},
	{label: "Reset local configuration", section: "MAINTENANCE", kind: resetConfigurationAction},
}

type screen int

const (
	menuScreen screen = iota
	confirmScreen
	pickerScreen
	storageScreen
	projectScreen
	resultScreen
)

type operationMsg struct {
	name     string
	err      error
	settings *config.Config
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
	choices        []models.Entry
	activeScreen   screen
	pendingAction  action
	input          textinput.Model
	result         string
	scrollOffset   int
	width          int
	height         int
}

// New creates a dashboard with persisted configuration and optional operations.
func New(saved config.Config, catalog []models.Entry, actions ...Actions) *Dashboard {
	input := textinput.New()
	input.SetWidth(72)
	dashboard := &Dashboard{config: saved, models: catalog, width: 80, height: 24, activity: "Ready", pickerAction: -1, input: input}
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
		if value.err == nil && value.settings != nil {
			dashboard.config = *value.settings
		}
		if value.err != nil {
			dashboard.result = fmt.Sprintf("%s failed: %v", value.name, value.err)
		} else {
			dashboard.result = value.name + " completed"
		}
		dashboard.activeScreen = resultScreen
		return dashboard, dashboard.refresh()
	case launchFinishedMsg:
		if value.err != nil {
			dashboard.result = "OpenCode failed: " + value.err.Error()
		} else {
			dashboard.result = "OpenCode session completed"
		}
		dashboard.activeScreen = resultScreen
		return dashboard, dashboard.refresh()
	case tea.KeyPressMsg:
		return dashboard.key(value)
	}
	return dashboard, nil
}

func (dashboard *Dashboard) key(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch dashboard.activeScreen {
	case resultScreen:
		if value.Code == tea.KeyEnter || value.Code == tea.KeyEscape {
			dashboard.activeScreen = menuScreen
		}
		return dashboard, nil
	case confirmScreen:
		return dashboard.confirm(value)
	case pickerScreen:
		return dashboard.pickModel(value)
	case storageScreen, projectScreen:
		return dashboard.textEntry(value)
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
		if dashboard.actionSelected+1 < len(actionDefinitions) {
			dashboard.actionSelected++
		}
	case tea.KeyEnter:
		return dashboard, dashboard.selectAction()
	}
	return dashboard, nil
}

func (dashboard *Dashboard) selectAction() tea.Cmd {
	switch actionDefinitions[dashboard.actionSelected].kind {
	case setupAction, startServerAction, stopServerAction, resetConfigurationAction:
		dashboard.pendingAction = actionDefinitions[dashboard.actionSelected].kind
		dashboard.activeScreen = confirmScreen
	case configureStorageAction:
		dashboard.openStorage()
	case downloadModelAction:
		if dashboard.config.ModelDirectory == "" {
			dashboard.openStorage()
			return nil
		}
		dashboard.openPicker(downloadModelAction, dashboard.models)
	case selectModelAction, deleteModelAction:
		dashboard.openPicker(actionDefinitions[dashboard.actionSelected].kind, dashboard.installedModels())
	case launchOpenCodeAction:
		dashboard.openProject()
	}
	return nil
}

func (dashboard *Dashboard) openPicker(kind action, choices []models.Entry) {
	dashboard.pickerAction = kind
	dashboard.choices = choices
	dashboard.modelSelected = 0
	dashboard.activeScreen = pickerScreen
}

func (dashboard *Dashboard) openStorage() {
	dashboard.activeScreen = storageScreen
	dashboard.input.SetValue(dashboard.config.ModelDirectory)
	dashboard.input.Focus()
}

func (dashboard *Dashboard) openProject() {
	dashboard.pendingAction = launchOpenCodeAction
	dashboard.activeScreen = projectScreen
	dashboard.input.SetValue("")
	dashboard.input.Focus()
}

func (dashboard *Dashboard) pickModel(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch value.Code {
	case tea.KeyEscape:
		dashboard.activeScreen = menuScreen
	case tea.KeyUp:
		if dashboard.modelSelected > 0 {
			dashboard.modelSelected--
		}
	case tea.KeyDown:
		if dashboard.modelSelected+1 < len(dashboard.choices) {
			dashboard.modelSelected++
		}
	case tea.KeyEnter:
		if model := dashboard.current().Name; model != "" {
			dashboard.pendingAction = dashboard.pickerAction
			dashboard.activeScreen = confirmScreen
		}
	}
	return dashboard, nil
}

func (dashboard *Dashboard) confirm(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if value.Code == tea.KeyEscape {
		dashboard.activeScreen = menuScreen
		return dashboard, nil
	}
	if value.Code != tea.KeyEnter {
		return dashboard, nil
	}
	model := dashboard.current().Name
	switch dashboard.pendingAction {
	case setupAction:
		return dashboard, dashboard.operation("Ollama setup", dashboard.actions.Setup)
	case startServerAction:
		return dashboard, dashboard.operation("Managed Ollama server started", dashboard.actions.Start)
	case stopServerAction:
		return dashboard, dashboard.operation("Managed Ollama server stopped", dashboard.actions.Stop)
	case downloadModelAction:
		return dashboard, dashboard.operation("Model download", func() error { return dashboard.callDownload(model) })
	case selectModelAction:
		return dashboard, dashboard.operation("Default model selected", func() error { return dashboard.callSelect(model) })
	case deleteModelAction:
		return dashboard, dashboard.operation("Model deleted", func() error { return dashboard.callDelete(model) })
	case resetConfigurationAction:
		return dashboard, dashboard.reset()
	}
	return dashboard, nil
}

func (dashboard *Dashboard) textEntry(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if value.Code == tea.KeyEscape {
		dashboard.activeScreen = menuScreen
		return dashboard, nil
	}
	if value.Code == tea.KeyEnter {
		value := strings.TrimSpace(dashboard.input.Value())
		if value == "" {
			dashboard.activity = "A folder is required"
			return dashboard, nil
		}
		if dashboard.activeScreen == storageScreen {
			if dashboard.actions.SetStorage == nil {
				dashboard.activity = "Storage configuration is unavailable"
				return dashboard, nil
			}
			return dashboard, func() tea.Msg {
				settings, err := dashboard.actions.SetStorage(value)
				return operationMsg{name: "Model storage configured", err: err, settings: &settings}
			}
		}
		return dashboard, dashboard.launchOpenCode(value)
	}
	var command tea.Cmd
	dashboard.input, command = dashboard.input.Update(value)
	return dashboard, command
}

func (dashboard *Dashboard) launchOpenCode(project string) tea.Cmd {
	if dashboard.actions.Launch == nil {
		dashboard.activity = "OpenCode launch is unavailable"
		return nil
	}
	command, err := dashboard.actions.Launch(project)
	if err != nil {
		dashboard.activity = "OpenCode failed: " + err.Error()
		return nil
	}
	dashboard.activeScreen = menuScreen
	dashboard.activity = "Running OpenCode..."
	return tea.ExecProcess(command, func(err error) tea.Msg { return launchFinishedMsg{err: err} })
}

func (dashboard *Dashboard) reset() tea.Cmd {
	if dashboard.actions.Reset == nil {
		return dashboard.operation("Configuration reset", nil)
	}
	return func() tea.Msg {
		settings, err := dashboard.actions.Reset()
		return operationMsg{name: "Configuration reset", err: err, settings: &settings}
	}
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
	if dashboard.modelSelected < 0 || dashboard.modelSelected >= len(dashboard.choices) {
		return models.Entry{}
	}
	return dashboard.choices[dashboard.modelSelected]
}

func (dashboard *Dashboard) installedModels() []models.Entry {
	entries := make([]models.Entry, 0, len(dashboard.snapshot.Installed))
	for name := range dashboard.snapshot.Installed {
		entries = append(entries, models.Entry{Name: name, Size: "installed"})
	}
	return entries
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
	if dashboard.activeScreen != menuScreen {
		view := tea.NewView(strings.Join([]string{title, "", dashboard.actionScreen()}, "\n"))
		view.AltScreen = true
		view.WindowTitle = "Local LLM"
		return view
	}
	hints := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(width).Align(lipgloss.Center).Render("↑/↓ Navigate  •  Enter select  •  Esc back  •  q quit")
	overview, resources := dashboard.statusPanels(width)
	actions, actionLine := dashboard.actionList()
	fixedHeight := lipgloss.Height(strings.Join([]string{title, hints, overview, resources}, "\n"))
	body, above, below := dashboard.actionViewport(actions, actionLine, max(1, dashboard.height-fixedHeight-2))
	view := tea.NewView(strings.Join([]string{title, hints, overview, resources, above, body, below}, "\n"))
	view.AltScreen = true
	view.WindowTitle = "Local LLM"
	return view
}

func (dashboard *Dashboard) actionScreen() string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(dashboard.screenTitle())
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	switch dashboard.activeScreen {
	case resultScreen:
		return strings.Join([]string{heading, "", dashboard.result, "", "[Enter] Back"}, "\n")
	case confirmScreen:
		return strings.Join([]string{heading, "", dashboard.confirmationDescription(), "", "[Enter] Confirm   [Esc] Cancel"}, "\n")
	case pickerScreen:
		lines := []string{heading, ""}
		if len(dashboard.choices) == 0 {
			lines = append(lines, "No installed models are available.")
		} else {
			for index, model := range dashboard.choices {
				marker := " "
				if index == dashboard.modelSelected {
					marker = "›"
				}
				lines = append(lines, fmt.Sprintf("%s %s  •  %s", marker, model.Name, model.Size))
			}
		}
		lines = append(lines, "", "[↑/↓] Navigate   [Enter] Continue   [Esc] Back")
		return strings.Join(lines, "\n")
	case storageScreen:
		return strings.Join([]string{heading, "", "Where should new Ollama models be stored?", "", dashboard.input.View(), dim.Render("Required: an absolute writable directory."), "", "[Enter] Save location   [Esc] Cancel"}, "\n")
	case projectScreen:
		return strings.Join([]string{heading, "", "Folder to open:", "", dashboard.input.View(), "", "[Enter] Start session   [Esc] Cancel"}, "\n")
	}
	return ""
}

func (dashboard *Dashboard) screenTitle() string {
	switch dashboard.activeScreen {
	case pickerScreen:
		if dashboard.pickerAction == downloadModelAction {
			return "DOWNLOAD A MODEL"
		}
		if dashboard.pickerAction == selectModelAction {
			return "CHOOSE DEFAULT MODEL"
		}
		return "DELETE A MODEL"
	case storageScreen:
		return "CONFIGURE MODEL STORAGE"
	case projectScreen:
		return "START OPENCODE SESSION"
	case confirmScreen:
		return strings.ToUpper(actionDefinitions[dashboard.actionIndex(dashboard.pendingAction)].label)
	case resultScreen:
		return strings.ToUpper(actionDefinitions[dashboard.actionIndex(dashboard.pendingAction)].label)
	}
	return "LOCAL LLM"
}

func (dashboard *Dashboard) confirmationDescription() string {
	switch dashboard.pendingAction {
	case setupAction:
		return "Install and configure Ollama for this user. This may request system permission."
	case startServerAction:
		return "Start the Ollama server managed by this dashboard."
	case stopServerAction:
		return "Stop only the Ollama server started by this dashboard. Downloaded models are kept."
	case resetConfigurationAction:
		return "Clear saved local-LLM settings and stop the managed server. Ollama and downloaded models are kept."
	case downloadModelAction:
		return "Download " + dashboard.current().Name + " to " + dashboard.config.ModelDirectory + "."
	case selectModelAction:
		return "Use " + dashboard.current().Name + " as the default local model."
	case deleteModelAction:
		return "Delete " + dashboard.current().Name + " from Ollama."
	}
	return "Confirm this action."
}

func (dashboard *Dashboard) actionIndex(target action) int {
	for index := range actionDefinitions {
		if actionDefinitions[index].kind == target {
			return index
		}
	}
	return 0
}

func (dashboard *Dashboard) statusPanels(width int) (string, string) {
	overview := dashboard.panel("OVERVIEW", fmt.Sprintf("Server: %s\nSelected: %s\nContext: %d tokens\nStorage: %s", empty(dashboard.snapshot.Server), empty(dashboard.config.SelectedModel), dashboard.config.ContextLength, dashboard.config.ModelDirectory), width)
	resources := dashboard.panel("RESOURCES", fmt.Sprintf("%s\nLoaded model VRAM: %s", empty(dashboard.snapshot.GPU), empty(dashboard.snapshot.LoadedVRAM)), width)
	if dashboard.height >= 19 {
		return overview, resources
	}
	return dashboard.statusLine("OVERVIEW", "Server: "+empty(dashboard.snapshot.Server)), dashboard.statusLine("RESOURCES", empty(dashboard.snapshot.GPU))
}

func (dashboard *Dashboard) statusLine(title, value string) string {
	firstLine := strings.Split(value, "\n")[0]
	maximum := max(8, dashboard.width-len(title)-4)
	runes := []rune(firstLine)
	if len(runes) > maximum {
		firstLine = string(runes[:maximum-1]) + "…"
	}
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(title)
	detail := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("  " + firstLine)
	return heading + detail
}

func (dashboard *Dashboard) actionViewport(content string, focusLine, height int) (string, string, string) {
	window := viewport.New(viewport.WithWidth(dashboard.width), viewport.WithHeight(height))
	window.FillHeight = true
	window.SetContent(content)
	window.SetYOffset(dashboard.scrollOffset)
	window.EnsureVisible(focusLine, 0, dashboard.width)
	dashboard.scrollOffset = window.YOffset()
	indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(dashboard.width).PaddingLeft(2)
	above := ""
	if !window.AtTop() {
		above = indicator.Render(fmt.Sprintf("↑ %d more", window.YOffset()))
	}
	below := ""
	if !window.AtBottom() {
		hiddenLines := window.TotalLineCount() - window.YOffset() - window.VisibleLineCount()
		below = indicator.Render(fmt.Sprintf("↓ %d more", max(1, hiddenLines)))
	}
	return window.View(), above, below
}

func (dashboard *Dashboard) actionList() (string, int) {
	lines := make([]string, 0, len(actionDefinitions)+8)
	focusLine := 0
	section := ""
	for index, definition := range actionDefinitions {
		if definition.section != section {
			section = definition.section
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, dashboard.sectionHeading(section))
		}
		line := "  " + definition.label
		if index == dashboard.actionSelected {
			line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Background(lipgloss.Color("24")).Padding(0, 1).Width(max(1, dashboard.width-4)).Render("› " + definition.label)
			focusLine = len(lines)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(dashboard.activity))
	return lipgloss.NewStyle().Width(dashboard.width).Padding(1, 2).Render(strings.Join(lines, "\n")), focusLine + 1
}

func (dashboard *Dashboard) sectionHeading(section string) string {
	lineLength := max(1, dashboard.width-len(section)-6)
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(section + " " + strings.Repeat("─", lineLength))
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

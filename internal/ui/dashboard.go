package ui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
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
	ModelError string
}

// Actions provides dashboard operations without coupling its view to system APIs.
type Actions struct {
	RepairSandbox func() (*exec.Cmd, error)
	Setup         func() error
	Start         func() error
	Stop          func() error
	Reset         func() (config.Config, error)
	SetStorage    func(string) (config.Config, error)
	SetContext    func(int) (config.Config, error)
	Download      func(string, func(DownloadProgress)) error
	Delete        func(string) error
	Select        func(string) error
	Launch        func(string) (*exec.Cmd, error)
	Refresh       func() Snapshot
	ListModels    func() ([]string, error)
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
	maestroGuideAction
	configureContextAction
	repairSandboxAction
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
	{label: "How do I run Maestro?", section: "WORK", kind: maestroGuideAction},
	{label: "Stop managed Ollama server", section: "MAINTENANCE", kind: stopServerAction},
	{label: "Reset local configuration", section: "MAINTENANCE", kind: resetConfigurationAction},
	{label: "Change context window", section: "MAINTENANCE", kind: configureContextAction},
	{label: "Repair Ubuntu Codex sandbox", section: "MAINTENANCE", kind: repairSandboxAction},
}

const modelRanking = `RTX 4090: 24 GB

1. huihui_ai/Qwen3.8-abliterated:27b
   Download: 18 GB
   Disk necessary: 25 GB
   Average VRAM: 22 GB
   Context: 16–32K
2. qwen3.8:27b
   Download: 18 GB
   Disk necessary: 25 GB
   Average VRAM: 22 GB
   Context: 16–32K
3. huihui_ai/Qwen3.6-abliterated:27b
   Download: 17 GB
   Disk necessary: 24 GB
   Average VRAM: 21 GB
   Context: 16–32K
4. huihui_ai/glm-4.7-flash-abliterated
   Download: 19 GB
   Disk necessary: 27 GB
   Average VRAM: 23 GB
   Context: 16–32K
5. qwen3-coder:30b
   Download: 19 GB
   Disk necessary: 27 GB
   Average VRAM: 20 GB
   Context: 16–32K
6. huihui_ai/gpt-oss-abliterated:20b
   Download: 14 GB
   Disk necessary: 20 GB
   Average VRAM: 22 GB
   Context: 32–64K
7. gpt-oss:20b
   Download: 14 GB
   Disk necessary: 20 GB
   Average VRAM: 22 GB
   Context: 32–64K

RTX 4070 Ti: 12 GB

1. huihui_ai/gemma-4-abliterated:12b
   Download: 7.6 GB
   Disk necessary: 11 GB
   Average VRAM: 10–12 GB
   Context: 16–32K
2. gemma4:12b
   Download: 7.7–8.0 GB
   Disk necessary: 12 GB
   Average VRAM: 10–12 GB
   Context: 16–32K
3. huihui_ai/qwen3.5-abliterated:9b
   Download: 6.6 GB
   Disk necessary: 10 GB
   Average VRAM: 8–10 GB
   Context: 16–32K
4. qwen3.5:9b
   Download: 6.6–7.6 GB
   Disk necessary: 11 GB
   Average VRAM: 8–10 GB
   Context: 16–32K

RTX 5070: 8 GB

1. huihui_ai/qwen3.5-abliterated:4B
   Download: 3.3 GB
   Disk necessary: 5 GB
   Average VRAM: 5–6 GB
   Context: 16–32K
2. qwen3.5:4b
   Download: 3.3–4.0 GB
   Disk necessary: 6 GB
   Average VRAM: 5–6 GB
   Context: 16–32K
3. huihui_ai/qwen3.5-abliterated:9b
   Download: 6.6 GB
   Disk necessary: 10 GB
   Average VRAM: 8–10 GB
   Context: 4–8K, tight fit
4. qwen3.5:9b
   Download: 6.6–7.6 GB
   Disk necessary: 11 GB
   Average VRAM: 8–10 GB
   Context: 4–8K, tight fit`

type screen int

const (
	menuScreen screen = iota
	confirmScreen
	pickerScreen
	rankingScreen
	storageScreen
	projectScreen
	downloadScreen
	resultScreen
	maestroGuideScreen
	modelLoadingScreen
	launchPreparingScreen
	contextScreen
)

type operationMsg struct {
	name     string
	err      error
	settings *config.Config
}

type refreshMsg Snapshot
type modelListMsg struct {
	kind  action
	names []string
	err   error
}
type launchFinishedMsg struct{ err error }
type launchPreparedMsg struct {
	command *exec.Cmd
	err     error
}
type sandboxRepairFinishedMsg struct{ err error }
type downloadProgressMsg struct {
	progress DownloadProgress
	updates  <-chan DownloadProgress
}

// DownloadProgress describes one streamed Ollama model download update.
type DownloadProgress struct {
	Status    string
	Completed int64
	Total     int64
}

// Dashboard presents local model setup and runtime state in the terminal.
type Dashboard struct {
	config          config.Config
	models          []models.Entry
	actions         Actions
	snapshot        Snapshot
	activity        string
	actionSelected  int
	modelSelected   int
	pickerAction    action
	choices         []models.Entry
	activeScreen    screen
	pendingAction   action
	input           textinput.Model
	result          string
	downloadStatus  DownloadProgress
	scrollOffset    int
	rankingOffset   int
	contextSelected int
	width           int
	height          int
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
	dashboard.input.SetWidth(max(1, dashboard.width-4))
}

// Init begins periodic non-blocking status refreshes.
func (dashboard *Dashboard) Init() tea.Cmd { return dashboard.refresh() }

// Update handles dashboard keyboard commands and asynchronous action results.
func (dashboard *Dashboard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case modelListMsg:
		if dashboard.activeScreen != modelLoadingScreen {
			return dashboard, nil
		}
		if value.err != nil {
			dashboard.result = "Read installed models failed: " + value.err.Error()
			dashboard.activeScreen = resultScreen
			return dashboard, nil
		}
		dashboard.snapshot.Installed = make(map[string]bool)
		for _, name := range value.names {
			dashboard.snapshot.Installed[name] = true
		}
		dashboard.openPicker(value.kind, dashboard.installedModels())
		return dashboard, nil
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
		} else if dashboard.pendingAction == configureContextAction {
			dashboard.result = fmt.Sprintf("Context window set to %d tokens. The managed server will use it when next started or used.", dashboard.config.ContextLength)
		} else {
			dashboard.result = value.name + " completed"
		}
		dashboard.activeScreen = resultScreen
		return dashboard, dashboard.refresh()
	case sandboxRepairFinishedMsg:
		dashboard.result = "Ubuntu sandbox profile loaded and Maestro sandbox verification passed."
		if value.err != nil {
			dashboard.result = "Sandbox repair failed. Review the terminal output above and resolve the reported error before retrying."
		}
		dashboard.activeScreen = resultScreen
		return dashboard, nil
	case launchPreparedMsg:
		if value.err != nil || value.command == nil {
			dashboard.result = "OpenCode failed: launch command is unavailable"
			if value.err != nil {
				dashboard.result = "OpenCode failed: " + value.err.Error()
			}
			dashboard.activeScreen = resultScreen
			return dashboard, nil
		}
		return dashboard, tea.ExecProcess(value.command, func(err error) tea.Msg { return launchFinishedMsg{err: err} })
	case launchFinishedMsg:
		if value.err != nil {
			dashboard.result = "OpenCode failed: " + value.err.Error()
		} else {
			dashboard.result = "OpenCode session completed"
		}
		dashboard.activeScreen = resultScreen
		return dashboard, dashboard.refresh()
	case downloadProgressMsg:
		dashboard.downloadStatus = value.progress
		return dashboard, waitForDownloadProgress(value.updates)
	case tea.KeyPressMsg:
		return dashboard.key(value)
	case tea.PasteMsg:
		return dashboard.paste(value)
	}
	return dashboard, nil
}

func (dashboard *Dashboard) paste(value tea.PasteMsg) (tea.Model, tea.Cmd) {
	if dashboard.activeScreen != storageScreen && dashboard.activeScreen != projectScreen {
		return dashboard, nil
	}
	var command tea.Cmd
	dashboard.input, command = dashboard.input.Update(value)
	return dashboard, command
}

func (dashboard *Dashboard) key(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch dashboard.activeScreen {
	case launchPreparingScreen:
		return dashboard, nil
	case modelLoadingScreen:
		if value.Code == tea.KeyEscape {
			dashboard.activeScreen = menuScreen
		}
		return dashboard, nil
	case resultScreen:
		if value.Code == tea.KeyEnter || value.Code == tea.KeyEscape {
			dashboard.activeScreen = menuScreen
		}
		return dashboard, nil
	case maestroGuideScreen:
		if value.Code == tea.KeyEnter || value.Code == tea.KeyEscape {
			dashboard.activeScreen = menuScreen
		}
		return dashboard, nil
	case confirmScreen:
		return dashboard.confirm(value)
	case pickerScreen:
		return dashboard.pickModel(value)
	case contextScreen:
		return dashboard.pickContext(value)
	case rankingScreen:
		return dashboard.navigateRanking(value)
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
	case setupAction, startServerAction, stopServerAction, resetConfigurationAction, repairSandboxAction:
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
		if dashboard.actions.ListModels != nil {
			kind := actionDefinitions[dashboard.actionSelected].kind
			dashboard.pendingAction = kind
			dashboard.result = "Loading installed models from configured Ollama storage…"
			dashboard.activeScreen = modelLoadingScreen
			return func() tea.Msg {
				names, err := dashboard.actions.ListModels()
				return modelListMsg{kind: kind, names: names, err: err}
			}
		}
		dashboard.openPicker(actionDefinitions[dashboard.actionSelected].kind, dashboard.installedModels())
	case launchOpenCodeAction:
		dashboard.openProject()
	case maestroGuideAction:
		dashboard.activeScreen = maestroGuideScreen
	case configureContextAction:
		dashboard.pendingAction = configureContextAction
		dashboard.contextSelected = 0
		if dashboard.config.ContextLength == 32768 {
			dashboard.contextSelected = 1
		}
		dashboard.activeScreen = contextScreen
	}
	return nil
}

func (dashboard *Dashboard) pickContext(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch value.Code {
	case tea.KeyEscape:
		dashboard.activeScreen = menuScreen
	case tea.KeyUp:
		dashboard.contextSelected = 0
	case tea.KeyDown:
		dashboard.contextSelected = 1
	case tea.KeyEnter:
		if dashboard.actions.SetContext == nil {
			return dashboard, dashboard.operation("Context window", nil)
		}
		length := []int{16384, 32768}[dashboard.contextSelected]
		return dashboard, func() tea.Msg {
			settings, err := dashboard.actions.SetContext(length)
			return operationMsg{name: "Context window update", err: err, settings: &settings}
		}
	}
	return dashboard, nil
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
	case '?':
		if dashboard.pickerAction == downloadModelAction {
			dashboard.rankingOffset = 0
			dashboard.activeScreen = rankingScreen
		}
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

func (dashboard *Dashboard) navigateRanking(value tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch value.Code {
	case '?', tea.KeyEscape:
		dashboard.activeScreen = pickerScreen
	case tea.KeyUp:
		dashboard.rankingOffset = max(0, dashboard.rankingOffset-1)
	case tea.KeyDown:
		dashboard.rankingOffset++
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
	case repairSandboxAction:
		if dashboard.actions.RepairSandbox == nil {
			return dashboard, dashboard.operation("Ubuntu sandbox repair", nil)
		}
		command, err := dashboard.actions.RepairSandbox()
		if err != nil {
			return dashboard, func() tea.Msg { return sandboxRepairFinishedMsg{err: err} }
		}
		return dashboard, tea.ExecProcess(command, func(err error) tea.Msg { return sandboxRepairFinishedMsg{err: err} })
	case setupAction:
		return dashboard, dashboard.operation("Ollama setup", dashboard.actions.Setup)
	case startServerAction:
		return dashboard, dashboard.operation("Managed Ollama server started", dashboard.actions.Start)
	case stopServerAction:
		return dashboard, dashboard.operation("Managed Ollama server stopped", dashboard.actions.Stop)
	case downloadModelAction:
		dashboard.activeScreen = downloadScreen
		dashboard.downloadStatus = DownloadProgress{Status: "Preparing download"}
		return dashboard, dashboard.download(model)
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
			dashboard.result = "A folder is required"
			dashboard.activeScreen = resultScreen
			return dashboard, nil
		}
		if dashboard.activeScreen == storageScreen {
			if dashboard.actions.SetStorage == nil {
				dashboard.activity = "Storage configuration is unavailable"
				return dashboard, nil
			}
			return dashboard, func() tea.Msg {
				settings, err := dashboard.actions.SetStorage(value)
				return operationMsg{name: "Model storage configured. The next managed server start will use this directory", err: err, settings: &settings}
			}
		}
		return dashboard, dashboard.launchOpenCode(value)
	}
	var command tea.Cmd
	dashboard.input, command = dashboard.input.Update(value)
	return dashboard, command
}

func (dashboard *Dashboard) launchOpenCode(project string) tea.Cmd {
	dashboard.activeScreen = launchPreparingScreen
	return func() tea.Msg {
		if dashboard.actions.Launch == nil {
			return launchPreparedMsg{err: fmt.Errorf("OpenCode launch is unavailable")}
		}
		command, err := dashboard.actions.Launch(project)
		return launchPreparedMsg{command: command, err: err}
	}
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

func (dashboard *Dashboard) download(model string) tea.Cmd {
	updates := make(chan DownloadProgress, 1)
	return tea.Batch(
		waitForDownloadProgress(updates),
		func() tea.Msg {
			err := dashboard.callDownload(model, func(progress DownloadProgress) {
				updates <- progress
			})
			close(updates)
			return operationMsg{name: "Model download", err: err}
		},
	)
}

func waitForDownloadProgress(updates <-chan DownloadProgress) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-updates
		if !ok {
			return nil
		}
		return downloadProgressMsg{progress: progress, updates: updates}
	}
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
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func (dashboard *Dashboard) callDownload(model string, report func(DownloadProgress)) error {
	if model == "" {
		return fmt.Errorf("select a catalog model first")
	}
	if dashboard.actions.Download == nil {
		return fmt.Errorf("model download is unavailable")
	}
	return dashboard.actions.Download(model, report)
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
	status := strings.TrimSuffix(strings.Join([]string{overview, resources}, "\n"), "\n")
	actions, actionLine := dashboard.actionList()
	fixedHeight := lipgloss.Height(strings.Join([]string{title, hints, status}, "\n"))
	body, above, below := dashboard.actionViewport(actions, actionLine, max(1, dashboard.height-fixedHeight-2))
	view := tea.NewView(strings.Join([]string{title, hints, status, above, body, below}, "\n"))
	view.AltScreen = true
	view.WindowTitle = "Local LLM"
	return view
}

func (dashboard *Dashboard) actionScreen() string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(dashboard.screenTitle())
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	switch dashboard.activeScreen {
	case launchPreparingScreen:
		return dashboard.wrap(heading + "\n\nPreparing OpenCode and checking the managed Ollama server…")
	case modelLoadingScreen:
		return dashboard.wrap(strings.Join([]string{heading, "", dashboard.result, "", "[Esc] Back"}, "\n"))
	case resultScreen:
		return dashboard.wrap(strings.Join([]string{heading, "", dashboard.result, "", "[Enter] Back"}, "\n"))
	case downloadScreen:
		return dashboard.downloadView(heading)
	case confirmScreen:
		return dashboard.wrap(strings.Join([]string{heading, "", dashboard.confirmationDescription(), "", "[Enter] Confirm   [Esc] Cancel"}, "\n"))
	case pickerScreen:
		lines := []string{heading, ""}
		if len(dashboard.choices) == 0 {
			lines = append(lines, "No installed models are available.")
		} else {
			for index, model := range dashboard.choices {
				lines = append(lines, dashboard.modelCard(index, model)...)
			}
		}
		help := ""
		if dashboard.pickerAction == downloadModelAction {
			help = "   [?] GPU rankings"
		}
		lines = append(lines, "", "[↑/↓] Navigate   [Enter] Continue   [Esc] Back"+help)
		return dashboard.wrap(strings.Join(lines, "\n"))
	case rankingScreen:
		return dashboard.rankingView()
	case storageScreen:
		return dashboard.wrap(strings.Join([]string{heading, "", "Where should new Ollama models be stored?", "", dashboard.input.View(), dim.Render("Required: an absolute writable directory."), "", "[Enter] Save location   [Esc] Cancel"}, "\n"))
	case projectScreen:
		model := dashboard.config.SelectedModel
		if model == "" {
			model = "MODEL_NAME"
		}
		settings, _ := json.Marshal(map[string]string{"model": "ollama/" + model})
		quotedSettings := "'" + strings.ReplaceAll(string(settings), "'", "'\"'\"'") + "'"
		return dashboard.wrap(strings.Join([]string{heading, "", "Folder to open:", "", dashboard.input.View(), "", "[Enter] Start session   [Esc] Cancel", "", dim.Render("Start manually in another terminal:"), dim.Render("  cd -- /path/to/project"), dim.Render("  export OPENCODE_CONFIG_CONTENT=" + quotedSettings), dim.Render("  \"$HOME/.opencode/bin/opencode\" --standalone"), dim.Render("Requires a running Ollama server and an installed model. Replace the folder and model as needed.")}, "\n"))
	case contextScreen:
		lines := []string{heading, "", "Choose the maximum context window for the managed Ollama server.", dim.Render("32K uses more memory. Running sessions keep their current setting."), ""}
		for index, length := range []int{16384, 32768} {
			marker := "  "
			if index == dashboard.contextSelected {
				marker = "› "
			}
			label := []string{"16,384 tokens (16K)", "32,768 tokens (32K)"}[index]
			if length == dashboard.config.ContextLength {
				label += "  •  Current"
			}
			lines = append(lines, marker+label)
		}
		return dashboard.wrap(strings.Join(append(lines, "", "[↑/↓] Navigate   [Enter] Save   [Esc] Cancel"), "\n"))
	case maestroGuideScreen:
		return dashboard.wrap(strings.Join([]string{
			heading,
			"",
			"Maestro plans and reviews; OpenCode implements and runs tests.",
			"",
			"Codex",
			"  codex --profile maestro",
			"",
			"Claude Code",
			"  claude --agent maestro",
			"",
			"Antigravity",
			"  Use Codex or Claude for worker delegation.",
			"",
			"Run from your project folder. Maestro delegates edits and tests to OpenCode.",
			"Reconnect: Ask Maestro to resume waiting for the saved job ID.",
			"",
			"[Enter] Back   [Esc] Back",
		}, "\n"))
	}
	return ""
}

func (dashboard *Dashboard) downloadView(heading string) string {
	lines := []string{heading, "", "Downloading " + dashboard.current().Name + " to " + dashboard.config.ModelDirectory + ".", "", "Status: " + dashboard.downloadStatus.Status}
	if dashboard.downloadStatus.Total > 0 {
		percent := dashboard.downloadStatus.Completed * 100 / dashboard.downloadStatus.Total
		lines = append(lines, fmt.Sprintf("Progress: %s / %s (%d%%)", formatBytes(dashboard.downloadStatus.Completed), formatBytes(dashboard.downloadStatus.Total), percent))
	}
	lines = append(lines, "", "Download in progress. Please wait.")
	return dashboard.wrap(strings.Join(lines, "\n"))
}

func formatBytes(value int64) string {
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if value >= gib {
		return fmt.Sprintf("%.1f GiB", float64(value)/gib)
	}
	if value >= mib {
		return fmt.Sprintf("%.0f MiB", float64(value)/mib)
	}
	return fmt.Sprintf("%d B", value)
}

func (dashboard *Dashboard) rankingView() string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render("MODEL RANKING BY GPU")
	content := dashboard.wrap(styleRanking(modelRanking))
	window := viewport.New(viewport.WithWidth(dashboard.width), viewport.WithHeight(max(1, dashboard.height-5)))
	window.SetContent(content)
	window.SetYOffset(dashboard.rankingOffset)
	dashboard.rankingOffset = window.YOffset()
	indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	lines := []string{heading, ""}
	if !window.AtTop() {
		lines = append(lines, indicator.Render(fmt.Sprintf("↑ %d more", window.YOffset())))
	}
	lines = append(lines, window.View())
	if !window.AtBottom() {
		hiddenLines := window.TotalLineCount() - window.YOffset() - window.VisibleLineCount()
		lines = append(lines, indicator.Render(fmt.Sprintf("↓ %d more", max(1, hiddenLines))))
	}
	lines = append(lines, "", "[↑/↓] Scroll   [?] or [Esc] Back")
	return strings.Join(lines, "\n")
}

func styleRanking(ranking string) string {
	groupStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	descriptionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	lines := strings.Split(ranking, "\n")
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "RTX "):
			lines[index] = groupStyle.Render(line)
		case len(line) > 3 && line[1] == '.' && line[2] == ' ':
			lines[index] = line[:3] + nameStyle.Render(line[3:])
		case strings.HasPrefix(line, "   "):
			lines[index] = line[:3] + descriptionStyle.Render(line[3:])
		}
	}
	return strings.Join(lines, "\n")
}

func (dashboard *Dashboard) modelCard(index int, model models.Entry) []string {
	marker := " "
	if index == dashboard.modelSelected {
		marker = "›"
	}
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	if index == dashboard.modelSelected {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}
	descriptionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	lines := dashboard.wrapIndented(marker+" ", nameStyle.Render(model.Name))
	if model.Size == "installed" {
		return append(lines, dashboard.wrapIndented("  ", descriptionStyle.Render("Installed on this machine"))...)
	}
	if model.Size != "" {
		lines = append(lines, dashboard.wrapIndented("  ", descriptionStyle.Render("Estimated download: "+model.Size))...)
	}
	if model.VRAM != "" {
		lines = append(lines, dashboard.wrapIndented("  ", descriptionStyle.Render("VRAM required: "+model.VRAM))...)
	}
	return lines
}

func (dashboard *Dashboard) wrapIndented(prefix, value string) []string {
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	wrapped := strings.Split(lipgloss.Wrap(value, max(1, dashboard.width-lipgloss.Width(prefix)), ""), "\n")
	for index := range wrapped {
		if index == 0 {
			wrapped[index] = prefix + wrapped[index]
		} else {
			wrapped[index] = indent + wrapped[index]
		}
	}
	return wrapped
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
	case projectScreen, launchPreparingScreen:
		return "START OPENCODE SESSION"
	case contextScreen:
		return "CHANGE CONTEXT WINDOW"
	case downloadScreen:
		return "DOWNLOADING A MODEL"
	case confirmScreen:
		return strings.ToUpper(actionDefinitions[dashboard.actionIndex(dashboard.pendingAction)].label)
	case resultScreen, modelLoadingScreen:
		return strings.ToUpper(actionDefinitions[dashboard.actionIndex(dashboard.pendingAction)].label)
	case maestroGuideScreen:
		return "HOW DO I RUN MAESTRO?"
	}
	return "LOCAL LLM"
}

func (dashboard *Dashboard) confirmationDescription() string {
	switch dashboard.pendingAction {
	case repairSandboxAction:
		return "Ubuntu only: install apparmor-profiles and apparmor-utils with sudo, copy and load the Bubblewrap AppArmor profile, then verify with codex --profile maestro sandbox -- /usr/bin/true. This changes the system AppArmor profile and may ask for your sudo password. Global user-namespace restrictions are kept enabled."
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
	overviewBody := fmt.Sprintf("Server: %s\nSelected: %s\nContext: %d tokens\nStorage: %s", empty(dashboard.snapshot.Server), empty(dashboard.config.SelectedModel), dashboard.config.ContextLength, dashboard.config.ModelDirectory)
	if dashboard.snapshot.ModelError != "" {
		overviewBody += "\nModel lookup failed: " + dashboard.snapshot.ModelError
	}
	resourcesBody := fmt.Sprintf("%s\nLoaded model VRAM: %s", empty(dashboard.snapshot.GPU), empty(dashboard.snapshot.LoadedVRAM))
	if dashboard.height >= 19 {
		if width >= 80 {
			overviewWidth := (width - 1) / 2
			resourcesWidth := width - overviewWidth - 1
			return lipgloss.JoinHorizontal(lipgloss.Top, dashboard.panel("OVERVIEW", overviewBody, overviewWidth), " ", dashboard.panel("RESOURCES", resourcesBody, resourcesWidth)), ""
		}
		return dashboard.panel("OVERVIEW", overviewBody, width), dashboard.panel("RESOURCES", resourcesBody, width)
	}
	return dashboard.statusLine("OVERVIEW", "Server: "+empty(dashboard.snapshot.Server)), dashboard.statusLine("RESOURCES", empty(dashboard.snapshot.GPU))
}

func (dashboard *Dashboard) statusLine(title, value string) string {
	firstLine := strings.Split(value, "\n")[0]
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Render(title)
	detail := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("  " + firstLine)
	return dashboard.wrap(heading + detail)
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
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1).Width(width).Render(heading + "\n" + lipgloss.Wrap(body, max(1, width-4), ""))
}

func (dashboard *Dashboard) wrap(text string) string {
	return lipgloss.Wrap(text, dashboard.width, "")
}

func empty(value string) string {
	if value == "" {
		return "Unavailable"
	}
	return value
}

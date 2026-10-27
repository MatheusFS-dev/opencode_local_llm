package ui

import (
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
)

func TestSandboxRepairRequiresConfirmationAndAllowsCancel(t *testing.T) {
	calls := 0
	dashboard := New(config.Config{ContextLength: 16384}, nil, Actions{RepairSandbox: func() (*exec.Cmd, error) { calls++; return exec.Command("true"), nil }})
	dashboard.Resize(100, 30)
	dashboard.actionSelected = dashboard.actionIndex(repairSandboxAction)
	_, command := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command != nil || calls != 0 || dashboard.activeScreen != confirmScreen {
		t.Fatal("repair started before confirmation")
	}
	if !strings.Contains(dashboard.View().Content, "sudo") {
		t.Fatal("missing system permission explanation")
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if calls != 0 || dashboard.activeScreen != menuScreen {
		t.Fatal("cancel started repair")
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, command = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if calls != 1 || command == nil {
		t.Fatal("confirmed repair did not open interactive terminal")
	}
	_, _ = dashboard.Update(sandboxRepairFinishedMsg{err: errors.New("exit status 1")})
	if dashboard.activeScreen != resultScreen || !strings.Contains(dashboard.View().Content, "failed") {
		t.Fatal("repair failure did not remain on focused result screen")
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if dashboard.activeScreen != menuScreen {
		t.Fatal("back did not return to menu")
	}
}

func TestDashboardShowsStatusPanelsAndActionList(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", SelectedModel: "local:4b", ContextLength: 16384}, []models.Entry{{Name: "local:4b", Size: "4 GB"}})
	dashboard.Resize(120, 40)
	view := dashboard.View().Content
	for _, expected := range []string{
		"LOCAL LLM",
		"↑/↓ Navigate",
		"OVERVIEW",
		"RESOURCES",
		"SETUP",
		"MODELS",
		"WORK",
		"MAINTENANCE",
		"Set up Ollama",
		"Start OpenCode session",
		"How do I run Maestro?",
		"Stop managed Ollama server",
		"Configure model storage",
		"Download a model",
		"Choose default model",
		"Delete a model",
		"Reset local configuration",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
	for _, removed := range []string{"ACTIONS"} {
		if strings.Contains(view, removed) {
			t.Fatalf("view unexpectedly contains %q:\n%s", removed, view)
		}
	}
}

func TestDashboardOffersContextWindowChoices(t *testing.T) {
	dashboard := New(config.Config{ContextLength: 16384}, nil)
	for index, item := range actionDefinitions {
		if item.label == "Change context window" {
			dashboard.actionSelected = index
			dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			view := dashboard.View().Content
			for _, expected := range []string{"CONTEXT WINDOW", "16,384", "32,768", "Current"} {
				if !strings.Contains(view, expected) {
					t.Fatalf("context picker missing %q:\n%s", expected, view)
				}
			}
			return
		}
	}
	t.Fatal("context window action is missing")
}

func TestDashboardSavesAndReopensContextWindow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	saved := config.Config{ModelDirectory: filepath.Join(root, "models"), SelectedModel: "local:4b", ContextLength: 16384}
	if err := config.Save(path, saved); err != nil {
		t.Fatal(err)
	}
	dashboard := New(saved, nil, Actions{SetContext: func(length int) (config.Config, error) {
		updated := saved
		updated.ContextLength = length
		if err := config.Save(path, updated); err != nil {
			return config.Config{}, err
		}
		saved = updated
		return updated, nil
	}})
	for _, length := range []int{32768, 16384} {
		dashboard.actionSelected = dashboard.actionIndex(configureContextAction)
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if length == 32768 {
			dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		} else {
			dashboard.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		}
		_, command := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if command == nil {
			t.Fatal("context selection did not save")
		}
		dashboard.Update(command())
		loaded, err := config.Load(path, config.Default(root))
		if err != nil {
			t.Fatal(err)
		}
		if loaded.ContextLength != length || loaded.SelectedModel != "local:4b" || loaded.ModelDirectory != saved.ModelDirectory {
			t.Fatalf("saved settings = %#v", loaded)
		}
		if !strings.Contains(dashboard.View().Content, "managed server") {
			t.Fatal("result must explain when the setting takes effect")
		}
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
}

func TestDashboardCancelsContextChange(t *testing.T) {
	called := false
	dashboard := New(config.Config{ContextLength: 16384}, nil, Actions{SetContext: func(int) (config.Config, error) {
		called = true
		return config.Config{}, nil
	}})
	dashboard.actionSelected = dashboard.actionIndex(configureContextAction)
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if called || dashboard.config.ContextLength != 16384 || dashboard.activeScreen != menuScreen {
		t.Fatal("cancel changed the context setting")
	}
}

func TestDashboardShowsMaestroQuickStartWithoutConfirmation(t *testing.T) {
	dashboard := New(config.Config{ContextLength: 16384}, nil)
	for range 7 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := dashboard.View().Content
	for _, expected := range []string{
		"HOW DO I RUN MAESTRO?",
		"codex --profile maestro",
		"claude --agent maestro",
		"Use Codex or Claude for worker delegation.",
		"[Enter] Back",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Maestro guide missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "[Enter] Confirm") {
		t.Fatalf("Maestro guide must be read-only, not a confirmation screen:\n%s", view)
	}
}

func TestDashboardKeepsOperationFailureOnFocusedResultScreen(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil, Actions{Setup: func() error {
		return errors.New("Ollama is already installed at /usr/local/bin/ollama")
	}})
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, command := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = dashboard.Update(command())
	view := dashboard.View().Content
	for _, expected := range []string{"SET UP OLLAMA", "Ollama setup failed: Ollama is already installed at /usr/local/bin/ollama", "[Enter] Back"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("result screen missing %q:\n%s", expected, view)
		}
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := dashboard.View().Content; !strings.Contains(view, "SETUP") {
		t.Fatalf("Enter must return to the main screen:\n%s", view)
	}
}

func TestDashboardShowsDownloadProgressWhilePullRuns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, []models.Entry{{Name: "catalog:4b", Size: "4 GB"}}, Actions{
		Download: func(_ string, report func(DownloadProgress)) error {
			close(started)
			report(DownloadProgress{Status: "downloading", Completed: 1024, Total: 2048})
			<-release
			return nil
		},
	})
	dashboard.openPicker(downloadModelAction, dashboard.models)
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, command := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	commands, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatal("starting a download must wait for progress and completion concurrently")
	}
	completed := make(chan tea.Msg, len(commands))
	for _, work := range commands {
		go func() { completed <- work() }()
	}
	<-started

	view := dashboard.View().Content
	for _, expected := range []string{"DOWNLOADING A MODEL", "Downloading catalog:4b", "Download in progress"} {
		if !strings.Contains(view, expected) {
			close(release)
			for range commands {
				<-completed
			}
			t.Fatalf("running download must show progress instead of the confirmation screen, missing %q:\n%s", expected, view)
		}
	}
	progress := <-completed
	dashboard.Update(progress)
	view = dashboard.View().Content
	for _, expected := range []string{"Status: downloading", "Progress: 1024 B / 2048 B (50%)"} {
		if !strings.Contains(view, expected) {
			close(release)
			<-completed
			t.Fatalf("running download must render streamed progress, missing %q:\n%s", expected, view)
		}
	}

	close(release)
	for range len(commands) - 1 {
		message := <-completed
		dashboard.Update(message)
	}
}

func TestDashboardRequiresStorageConfigurationBeforeDownload(t *testing.T) {
	dashboard := New(config.Config{ContextLength: 16384}, []models.Entry{{Name: "catalog:4b", Size: "4 GB"}})
	for range 3 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := dashboard.View().Content
	for _, expected := range []string{"CONFIGURE MODEL STORAGE", "absolute writable directory"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("storage setup screen missing %q:\n%s", expected, view)
		}
	}
}

func TestDashboardShowsLabeledModelRequirements(t *testing.T) {
	catalog, err := models.Load(filepath.Join("..", "..", "src", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, catalog)
	dashboard.openPicker(downloadModelAction, catalog)
	view := dashboard.View().Content
	for _, expected := range []string{
		"Estimated download: about 3.3 GB",
		"VRAM required: 8 GB for 16–32K context",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("download picker missing %q:\n%s", expected, view)
		}
	}
}

func TestDashboardStylesModelPickerCardsBySelection(t *testing.T) {
	catalog := []models.Entry{
		{Name: "selected:4b", Size: "4 GB", VRAM: "8 GB"},
		{Name: "available:9b", Size: "8 GB", VRAM: "12 GB"},
	}
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, catalog)
	dashboard.openPicker(downloadModelAction, catalog)
	view := dashboard.View().Content

	greenName := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("selected:4b")
	whiteName := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render("available:9b")
	grayDescription := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("Estimated download: 4 GB")
	for _, expected := range []string{greenName, whiteName, grayDescription} {
		if !strings.Contains(view, expected) {
			t.Fatalf("model picker missing styled content %q:\n%s", expected, view)
		}
	}
}

func TestDashboardShowsGPUModelRankingFromDownloadPicker(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, []models.Entry{{Name: "catalog:4b"}})
	dashboard.openPicker(downloadModelAction, dashboard.models)
	dashboard.Update(tea.KeyPressMsg{Code: '?'})
	view := dashboard.View().Content
	for _, expected := range []string{
		"MODEL RANKING BY GPU",
		"RTX 4090: 24 GB",
		"huihui_ai/Qwen3.8-abliterated:27b",
		"[?] or [Esc] Back",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("GPU ranking missing %q:\n%s", expected, view)
		}
	}
	for _, unexpected := range []string{"Priority: coding-agent reliability", "Best first choice for coding and reasoning", "Compare for stronger instruction following"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("GPU ranking must contain only ranking data, found %q:\n%s", unexpected, view)
		}
	}
	for _, expected := range []string{
		lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render("huihui_ai/Qwen3.8-abliterated:27b"),
		"\x1b[38;5;244mDownload: 18 GB",
		"Disk necessary: 25 GB",
		"Average VRAM: 22 GB",
		"Context: 16–32K",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("GPU ranking missing formatted field %q:\n%s", expected, view)
		}
	}
	verticalFields := regexp.MustCompile(`(?s)\x1b\[38;5;244mDownload: 18 GB\x1b\[m[^\n]*\n   \x1b\[38;5;244mDisk necessary: 25 GB\x1b\[m[^\n]*\n   \x1b\[38;5;244mAverage VRAM: 22 GB\x1b\[m[^\n]*\n   \x1b\[38;5;244mContext: 16–32K\x1b\[m`)
	if !verticalFields.MatchString(view) {
		t.Fatalf("GPU ranking must render model requirements as a vertical list:\n%s", view)
	}
	for _, unexpected := range []string{" •  A", " •  S", "Context to test:"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("GPU ranking contains removed format %q:\n%s", unexpected, view)
		}
	}
	for range 40 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view = dashboard.View().Content
	if !strings.Contains(view, "huihui_ai/gemma-4-abliterated:12b") {
		t.Fatalf("scrolled GPU ranking missing the RTX 4070 Ti entries:\n%s", view)
	}
	for range 20 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view = dashboard.View().Content
	for _, expected := range []string{"RTX 5070: 8 GB"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("scrolled GPU ranking missing %q:\n%s", expected, view)
		}
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if view := dashboard.View().Content; !strings.Contains(view, "DOWNLOAD A MODEL") {
		t.Fatalf("Esc must return to the download picker:\n%s", view)
	}
}

func TestDashboardAcceptsPastedStoragePath(t *testing.T) {
	dashboard := New(config.Config{ContextLength: 16384}, nil)
	dashboard.openStorage()
	dashboard.Update(tea.PasteMsg{Content: "/media/models"})
	if got := dashboard.input.Value(); got != "/media/models" {
		t.Fatalf("pasted storage path = %q, want %q", got, "/media/models")
	}
}

func TestDashboardReloadsModelsBeforeOpeningPicker(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models"}, nil, Actions{
		ListModels: func() ([]string, error) { return []string{"downloaded:4b"}, nil },
	})
	dashboard.actionSelected = 4
	command := dashboard.selectAction()
	if command == nil {
		t.Fatal("model picker did not request a fresh installed list")
	}
	dashboard.Update(command())
	if !strings.Contains(dashboard.View().Content, "downloaded:4b") {
		t.Fatal("freshly installed model missing")
	}
}

func TestDashboardReportsModelLookupFailure(t *testing.T) {
	dashboard := New(config.Config{}, nil, Actions{
		ListModels: func() ([]string, error) { return nil, errors.New("server unavailable") },
	})
	dashboard.actionSelected = 4
	command := dashboard.selectAction()
	if command == nil {
		t.Fatal("missing model lookup")
	}
	dashboard.Update(command())
	if view := dashboard.View().Content; !strings.Contains(view, "server unavailable") || strings.Contains(view, "No installed models") {
		t.Fatalf("lookup failure hidden: %s", view)
	}
}

func TestDashboardCancelledModelLookupDoesNotReopenPicker(t *testing.T) {
	dashboard := New(config.Config{}, nil, Actions{
		ListModels: func() ([]string, error) { return []string{"local:4b"}, nil },
	})
	dashboard.actionSelected = 4
	command := dashboard.selectAction()
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	dashboard.Update(command())
	if dashboard.activeScreen != menuScreen {
		t.Fatal("cancelled lookup reopened the picker")
	}
}

func TestDashboardShowsOnlyInstalledModelsForDefaultSelection(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, []models.Entry{{Name: "catalog-only:4b", Size: "4 GB"}})
	dashboard.snapshot.Installed = map[string]bool{"installed:4b": true}
	for range 4 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := dashboard.View().Content
	for _, expected := range []string{"CHOOSE DEFAULT MODEL", "installed:4b"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("default-model screen missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "catalog-only:4b") {
		t.Fatalf("default-model screen must not show catalog-only models:\n%s", view)
	}
}

func TestDashboardAsksForProjectFolderBeforeStartingOpenCode(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	for range 6 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := dashboard.View().Content
	for _, expected := range []string{"START OPENCODE SESSION", "Folder to open"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("project screen missing %q:\n%s", expected, view)
		}
	}
}

func TestDashboardScrollsToKeepTheSelectedActionVisible(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(60, 12)
	for range len(actionDefinitions) - 1 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := dashboard.View().Content
	for _, expected := range []string{"OVERVIEW", "RESOURCES", "Reset local configuration"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("short view missing %q after selecting the final action:\n%s", expected, view)
		}
	}
	if !regexp.MustCompile(`↑ [1-9][0-9]* more`).MatchString(view) {
		t.Fatalf("short view must show the number of hidden lines above:\n%s", view)
	}
}

func TestDashboardShowsLiveSnapshotAndInstalledSelection(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, []models.Entry{{Name: "local:4b", Size: "4 GB"}}, Actions{Refresh: func() Snapshot {
		return Snapshot{Server: "running", GPU: "NVIDIA GPU", LoadedVRAM: "2 GB", Installed: map[string]bool{"local:4b": true}}
	}})
	_, command := dashboard.Update(refreshMsg(dashboard.readSnapshot()))
	_ = command
	view := dashboard.View().Content
	for _, expected := range []string{"running", "NVIDIA GPU", "2 GB"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
	for range 4 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := dashboard.View().Content; !strings.Contains(view, "Installed on this machine") {
		t.Fatalf("model picker must identify installed models:\n%s", view)
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, command = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if message := command(); message.(operationMsg).err == nil {
		t.Fatal("selection succeeded without a selection action")
	}
}

func TestDashboardStacksPanelsInNarrowTerminal(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(40, 30)
	view := dashboard.View().Content
	if strings.Index(view, "OVERVIEW") > strings.Index(view, "RESOURCES") {
		t.Fatalf("narrow view must show overview before resources:\n%s", view)
	}
}

func TestDashboardPlacesStatusPanelsSideBySideInWideTerminal(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(120, 40)
	view := dashboard.View().Content
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "OVERVIEW") && strings.Contains(line, "RESOURCES") {
			return
		}
	}
	t.Fatalf("wide view must place overview and resources side by side:\n%s", view)
}

func TestDashboardWrapsFocusedScreenTextToTerminalWidth(t *testing.T) {
	dashboard := New(config.Config{ContextLength: 16384}, nil)
	dashboard.Resize(30, 24)
	dashboard.activeScreen = confirmScreen
	dashboard.pendingAction = resetConfigurationAction
	for _, line := range strings.Split(dashboard.View().Content, "\n") {
		if width := lipgloss.Width(line); width > 30 {
			t.Fatalf("line width = %d, want at most 30: %q", width, line)
		}
	}
}

func TestDashboardWrapsMainScreenTextToTerminalWidth(t *testing.T) {
	dashboard := New(config.Config{
		ModelDirectory: "/media/matheus.f@local.inatel.br/ssd01/models/very-long-model-storage-directory",
		SelectedModel:  "ollama/huihui_ai/qwen3.5-abliterated:4B",
		ContextLength:  16384,
	}, nil)
	dashboard.snapshot = Snapshot{GPU: "NVIDIA GeForce RTX 4070 with a long driver status description"}
	dashboard.Resize(30, 40)
	for _, line := range strings.Split(dashboard.View().Content, "\n") {
		if width := lipgloss.Width(line); width > 30 {
			t.Fatalf("line width = %d, want at most 30: %q", width, line)
		}
	}
}

func TestOpenCodeLaunchPreparesAsynchronouslyAndShowsFailure(t *testing.T) {
	calls := 0
	dashboard := New(config.Config{SelectedModel: "qwen3.5:9b"}, nil, Actions{Launch: func(project string) (*exec.Cmd, error) {
		calls++
		if project != "./" {
			t.Errorf("project = %q", project)
		}
		return nil, errors.New("Ollama is unavailable")
	}})
	dashboard.openProject()
	dashboard.input.SetValue("./ ")
	_, command := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil || calls != 0 {
		t.Fatal("launch preparation must run asynchronously")
	}
	if !strings.Contains(dashboard.View().Content, "Preparing OpenCode") {
		t.Fatal("preparation not visible")
	}
	dashboard.Update(command())
	if dashboard.activeScreen != resultScreen || !strings.Contains(dashboard.View().Content, "Ollama is unavailable") {
		t.Fatalf("launch failure hidden: %s", dashboard.View().Content)
	}
	dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if dashboard.activeScreen != menuScreen {
		t.Fatal("cannot return from failure")
	}
}

func TestOpenCodeProjectPromptShowsManualGuide(t *testing.T) {
	dashboard := New(config.Config{SelectedModel: "qwen3.5:9b"}, nil)
	dashboard.Resize(100, 40)
	dashboard.openProject()
	for _, text := range []string{"Start manually", "cd --", "opencode", "ollama/qwen3.5:9b"} {
		if !strings.Contains(dashboard.View().Content, text) {
			t.Errorf("manual guide missing %q", text)
		}
	}
}

func TestOpenCodePreparedSessionStartsInteractiveProcess(t *testing.T) {
	dashboard := New(config.Config{}, nil, Actions{Launch: func(string) (*exec.Cmd, error) { return exec.Command("true"), nil }})
	dashboard.openProject()
	dashboard.input.SetValue("./")
	_, prepare := dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, launch := dashboard.Update(prepare())
	if launch == nil {
		t.Fatal("prepared session did not start interactive process")
	}
	dashboard.Update(launchFinishedMsg{})
	if dashboard.activeScreen != resultScreen || !strings.Contains(dashboard.View().Content, "OpenCode session completed") {
		t.Fatal("session completion not visible")
	}
}

package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
)

// Dashboard presents local model setup and runtime state in the terminal.
type Dashboard struct {
	config config.Config
	models []models.Entry
	width  int
	height int
}

// New creates a dashboard with the persisted local model configuration.
func New(saved config.Config, catalog []models.Entry) *Dashboard {
	return &Dashboard{config: saved, models: catalog, width: 80, height: 24}
}

// Resize updates the dashboard dimensions.
func (dashboard *Dashboard) Resize(width, height int) {
	dashboard.width = max(20, width)
	dashboard.height = max(8, height)
}

// Init starts the Bubble Tea model without background work.
func (dashboard *Dashboard) Init() tea.Cmd {
	return nil
}

// Update accepts terminal resize and quit keys.
func (dashboard *Dashboard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		dashboard.Resize(value.Width, value.Height)
	case tea.KeyPressMsg:
		if value.Code == 'q' || (value.Code == 'c' && value.Mod.Contains(tea.ModCtrl)) {
			return dashboard, tea.Quit
		}
	}
	return dashboard, nil
}

// View renders an alternate-screen local LLM dashboard.
func (dashboard *Dashboard) View() tea.View {
	width := dashboard.width
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")).Width(width).Align(lipgloss.Center).Render("LOCAL LLM")
	overview := dashboard.panel("OVERVIEW", fmt.Sprintf("Selected model: %s\nContext: %d tokens\nStorage: %s", empty(dashboard.config.SelectedModel), dashboard.config.ContextLength, dashboard.config.ModelDirectory), dashboard.panelWidth())
	resources := dashboard.panel("RESOURCES", "GPU telemetry: unavailable until Ollama is running\nLoaded model VRAM: unavailable", dashboard.panelWidth())
	modelNames := make([]string, 0, len(dashboard.models))
	for _, model := range dashboard.models {
		modelNames = append(modelNames, fmt.Sprintf("%s  •  %s", model.Name, model.Size))
	}
	modelsPanel := dashboard.panel("MODELS", empty(strings.Join(modelNames, "\n")), dashboard.panelWidth())
	actions := dashboard.panel("ACTIONS", "[S] Set up Ollama   [R] Start server   [X] Stop server\n[D] Download model  [O] Run OpenCode   [Q] Quit", dashboard.panelWidth())
	var body string
	if width >= 86 {
		body = lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, overview, " ", resources), lipgloss.JoinHorizontal(lipgloss.Top, modelsPanel, " ", actions))
	} else {
		body = lipgloss.JoinVertical(lipgloss.Left, overview, resources, modelsPanel, actions)
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Width(width).Render("Configure models and Ollama locally • q quit")
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
		return "Not selected"
	}
	return value
}

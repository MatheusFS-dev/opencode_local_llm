package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
)

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
		"Stop managed Ollama server",
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

func TestDashboardScrollsToKeepTheSelectedActionVisible(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(60, 12)
	for range len(actionDefinitions) - 1 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := dashboard.View().Content
	for _, expected := range []string{"Reset local configuration"} {
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
	for range 3 {
		dashboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	_, _ = dashboard.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := dashboard.View().Content; !strings.Contains(view, "installed") {
		t.Fatalf("model picker must identify installed models:\n%s", view)
	}
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

func TestDashboardShowsOverviewBeforeResourcesInWideTerminal(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(120, 40)
	view := dashboard.View().Content
	if strings.Index(view, "OVERVIEW") > strings.Index(view, "RESOURCES") {
		t.Fatalf("wide view must show overview before resources:\n%s", view)
	}
}

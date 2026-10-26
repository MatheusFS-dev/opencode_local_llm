package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/models"
)

func TestDashboardShowsLocalLLMSectionsAndSelection(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", SelectedModel: "local:4b", ContextLength: 16384}, []models.Entry{{Name: "local:4b", Size: "4 GB"}})
	dashboard.Resize(120, 40)
	view := dashboard.View().Content
	for _, expected := range []string{"LOCAL LLM", "OVERVIEW", "RESOURCES", "MODELS", "ACTIONS", "local:4b"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
}

func TestDashboardShowsLiveSnapshotAndInstalledSelection(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, []models.Entry{{Name: "local:4b", Size: "4 GB"}}, Actions{Refresh: func() Snapshot {
		return Snapshot{Server: "running", GPU: "NVIDIA GPU", LoadedVRAM: "2 GB", Installed: map[string]bool{"local:4b": true}}
	}})
	_, command := dashboard.Update(refreshMsg(dashboard.readSnapshot()))
	_ = command
	view := dashboard.View().Content
	for _, expected := range []string{"running", "NVIDIA GPU", "2 GB", "installed"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q:\n%s", expected, view)
		}
	}
	_, command = dashboard.Update(tea.KeyPressMsg{Code: 'e'})
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

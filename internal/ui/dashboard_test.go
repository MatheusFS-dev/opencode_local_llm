package ui

import (
	"strings"
	"testing"

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

func TestDashboardStacksPanelsInNarrowTerminal(t *testing.T) {
	dashboard := New(config.Config{ModelDirectory: "/models", ContextLength: 16384}, nil)
	dashboard.Resize(40, 30)
	view := dashboard.View().Content
	if strings.Index(view, "OVERVIEW") > strings.Index(view, "RESOURCES") {
		t.Fatalf("narrow view must show overview before resources:\n%s", view)
	}
}

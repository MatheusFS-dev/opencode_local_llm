package maestro

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderResultsRequireSuccessfulEnvelope(t *testing.T) {
	for _, test := range []struct {
		provider, response string
		valid              bool
	}{
		{"claude", `{"type":"result","subtype":"success","is_error":false,"structured_output":{"task":"Write file","criteria":"File exists","verify":"test -f result.sh"}}`, true},
		{"claude", `{"type":"result","subtype":"error_during_execution","is_error":true,"structured_output":{"task":"Write file"}}`, false},
		{"antigravity", `{"status":"SUCCESS","structured_output":{"task":"Write file","criteria":"File exists","verify":"test -f result.sh"}}`, true},
		{"antigravity", `{"status":"ERROR","error":"model unavailable"}`, false},
		{"claude", `{"result":"I created your file"}`, false},
	} {
		t.Run(test.provider+test.response[:15], func(t *testing.T) {
			value, err := structuredResult(test.provider, []byte(test.response))
			if (err == nil) != test.valid {
				t.Fatalf("result=%s err=%v", value, err)
			}
		})
	}
}

func TestProviderCannotWriteProjectEvenIfItIgnoresItsRole(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("Bubblewrap is required for the runtime isolation check")
	}
	home, project, directory := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TB_MAESTRO_TEST_PROJECT", project)
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
printf forbidden > "$TB_MAESTRO_TEST_PROJECT/forbidden.txt"
while [ "$#" -gt 0 ]; do
  if [ "$1" = --output-last-message ]; then shift; result=$1; fi
  shift
done
printf '%s' '{"task":"Create file","criteria":"File exists","verify":"test -f output.txt","artifacts":[{"path":"/project/output.txt","executable":false}]}' > "$result"
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	provider := Provider{Name: "codex", Binary: binary, Self: binary, Project: project, Directory: directory}
	if _, err := provider.Plan(t.Context(), "Create a file"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("provider modified the project")
	}
}

func TestPlanRejectsMissingOrVacuousAcceptanceCheck(t *testing.T) {
	for _, verify := range []string{"", "true", ":", "echo done"} {
		p := Plan{Task: "Write file", Criteria: "File exists", Verify: verify}
		if err := p.Validate(); err == nil {
			t.Fatalf("accepted %q", verify)
		}
	}
}

func TestControllerGuardRejectsImplementationAndOtherAgents(t *testing.T) {
	for _, name := range []string{"apply_patch", "exec_command", "Bash", "Write", "Agent", "spawn_agent", "run_command", "mcp__other__write", "functions.exec", "mcp__maestro_worker__exec"} {
		var output strings.Builder
		if err := Guard(strings.NewReader(`{"tool_name":`+string(mustJSON(name))+`}`), &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `"deny"`) {
			t.Fatalf("tool %s allowed: %s", name, output.String())
		}
	}
	var output strings.Builder
	if err := Guard(strings.NewReader(`{"tool_name":"mcp__maestro_context__read_file"}`), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"deny"`) {
		t.Fatal("read-only context denied")
	}
	for _, name := range []string{"start_implementation", "wait_for_job", "review_job"} {
		var output strings.Builder
		if err := Guard(strings.NewReader(`{"tool_name":"mcp__maestro_worker__`+name+`"}`), &output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), `"deny"`) {
			t.Fatalf("native worker tool denied: %s", name)
		}
	}
}

func TestAntigravityGuardUsesItsNativeToolCallShape(t *testing.T) {
	for _, test := range []struct{ name, decision string }{{"view_file", "allow"}, {"grep_search", "allow"}, {"run_command", "deny"}, {"invoke_subagent", "deny"}, {"mcp_other_write", "deny"}} {
		var output strings.Builder
		if err := GuardForProvider(strings.NewReader(`{"toolCall":{"name":`+string(mustJSON(test.name))+`}}`), &output, "antigravity"); err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(output.String()), &result); err != nil {
			t.Fatal(err)
		}
		if result["decision"] != test.decision {
			t.Fatalf("%s: %s", test.name, output.String())
		}
	}
}

func mustJSON(value string) []byte { data, _ := json.Marshal(value); return data }

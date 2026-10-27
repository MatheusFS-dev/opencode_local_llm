package maestro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

// Plan is a bounded local implementation task with independent acceptance checks.
type Plan struct {
	Task      string            `json:"task"`
	Criteria  string            `json:"criteria"`
	Verify    string            `json:"verify"`
	Artifacts []worker.Artifact `json:"artifacts"`
}

// Validate rejects incomplete plans and trivial checks that cannot verify an artifact.
func (plan Plan) Validate() error {
	verify := strings.TrimSpace(plan.Verify)
	if strings.TrimSpace(plan.Task) == "" || strings.TrimSpace(plan.Criteria) == "" || verify == "" {
		return fmt.Errorf("planner must provide task, criteria, and an independent verification command")
	}
	if verify == "true" || verify == ":" || strings.HasPrefix(verify, "echo ") || strings.HasPrefix(verify, "printf ") {
		return fmt.Errorf("verification must check the actual deliverable, not a no-op or printed claim")
	}
	if len(plan.Artifacts) == 0 {
		return fmt.Errorf("planner must declare the actual deliverable files, separately from its shell acceptance checks")
	}
	for _, artifact := range plan.Artifacts {
		if !filepath.IsAbs(artifact.Path) {
			return fmt.Errorf("declared deliverables must use absolute file paths")
		}
	}
	return nil
}

// Review is a structured controller decision, separate from worker completion.
type Review struct {
	Approved   *bool  `json:"approved"`
	Reason     string `json:"reason"`
	Correction string `json:"correction"`
}

// Provider runs the installed CLI with native read-only tools and filesystem isolation.
type Provider struct {
	Name      string
	Binary    string
	Self      string
	Project   string
	Directory string
	Evidence  string
	ReadRoots []string
}

const planSchema = `{"type":"object","properties":{"task":{"type":"string"},"criteria":{"type":"string"},"verify":{"type":"string"},"artifacts":{"type":"array","minItems":1,"items":{"type":"object","properties":{"path":{"type":"string"},"executable":{"type":"boolean"}},"required":["path","executable"],"additionalProperties":false}}},"required":["task","criteria","verify","artifacts"],"additionalProperties":false}`
const reviewSchema = `{"type":"object","properties":{"approved":{"type":"boolean"},"reason":{"type":"string"},"correction":{"type":"string"}},"required":["approved","reason","correction"],"additionalProperties":false}`

// Plan inspects the request through a provider and validates its executable acceptance contract.
func (provider Provider) Plan(ctx context.Context, task string) (Plan, error) {
	task += "\nAuthorized deliverable directories: " + strings.Join(append([]string{provider.Project}, provider.ReadRoots...), ", ") + ". Do not declare unrelated existing host files as deliverables."
	prompt := "You are Maestro in a supervised PLANNING phase. Only inspect existing files. Do not implement, delegate, invoke a worker, write a helper, or run tests. Toolbox will run OpenCode after you return a JSON plan. Define a bounded task and an independent shell acceptance command that fails for missing/invalid deliverables. The acceptance command runs in a read-only filesystem with a private writable $TMPDIR. It must never implement or modify the deliverable. Use absolute paths for files outside the project. Runtime checks must use a timeout with a forced kill deadline and capture noisy output under $TMPDIR. Declare the actual requested deliverable file paths in artifacts, with executable=true only when required. Do not prescribe exact source bytes or embed a completed implementation. Verify behavior, allowing equivalent implementations. Do not use no-op checks. Project: " + provider.Project + "\nUser request:\n" + task
	data, err := provider.query(ctx, "plan", prompt, planSchema)
	if err != nil {
		return Plan{}, err
	}
	var plan Plan
	if err := decodeStrict(data, &plan); err != nil {
		return Plan{}, fmt.Errorf("invalid planner result: %w", err)
	}
	return plan, plan.Validate()
}

// Review inspects the actual files, worker log, and acceptance outcome before approving.
func (provider Provider) Review(ctx context.Context, job Job, directory string) (Review, error) {
	provider.Evidence = directory
	request, _ := json.Marshal(job.Request)
	prompt := "You are Maestro in a supervised REVIEW phase. The local OpenCode worker and independent acceptance command have exited successfully. Inspect the actual requested files and worker.log. Do not modify files, invoke a worker, run tests, or delegate. Return JSON with approved, reason, and correction. Approve only if the actual deliverable meets the user's request. If incorrect, give one focused correction for OpenCode. A passing syntax check alone does not establish runtime correctness. Project: " + provider.Project + "\nOriginal user request:\n" + job.UserTask + "\nTask and acceptance:\n" + string(request) + "\nJob evidence directory: " + directory
	data, err := provider.query(ctx, "review", prompt, reviewSchema)
	if err != nil {
		return Review{}, err
	}
	var review Review
	if err := decodeStrict(data, &review); err != nil {
		return Review{}, fmt.Errorf("invalid review result: %w", err)
	}
	if review.Approved == nil || strings.TrimSpace(review.Reason) == "" || (!*review.Approved && strings.TrimSpace(review.Correction) == "") {
		return Review{}, fmt.Errorf("review requires an explicit approval decision, a reason, and a correction when rejected")
	}
	return review, nil
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected data after JSON result")
	}
	return nil
}

func (provider Provider) query(ctx context.Context, phase, prompt, schema string) ([]byte, error) {
	directory, err := os.MkdirTemp(provider.Directory, "provider-"+phase+"-")
	if err != nil {
		return nil, err
	}
	schemaPath := filepath.Join(directory, "schema.json")
	resultPath := filepath.Join(directory, "result.json")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		return nil, err
	}
	guard := shellQuote(provider.Self) + " maestro-guard"
	var arguments []string
	switch provider.Name {
	case "codex":
		contextArguments := []string{"maestro-context", "--root", provider.Project, "--root", provider.Directory}
		if provider.Evidence != "" {
			contextArguments = append(contextArguments, "--root", provider.Evidence)
		}
		for _, root := range provider.ReadRoots {
			contextArguments = append(contextArguments, "--root", root)
		}
		encodedArguments, _ := json.Marshal(contextArguments)
		contextConfig := "mcp_servers={maestro_context={command=" + strconv.Quote(provider.Self) + ",args=" + string(encodedArguments) + ",startup_timeout_sec=15,tool_timeout_sec=30}}"
		hook := "hooks.PreToolUse=[{matcher=\"*\",hooks=[{type=\"command\",command=" + strconv.Quote(guard) + ",timeout=5}]}]"
		arguments = []string{"exec", "--profile", "maestro", "--sandbox", "read-only", "-c", "approval_policy=\"never\"", "--disable", "shell_tool", "--disable", "multi_agent", "--disable", "apps", "--dangerously-bypass-hook-trust", "-c", contextConfig, "-c", hook, "--skip-git-repo-check", "--output-schema", schemaPath, "--output-last-message", resultPath, "--json", prompt}
	case "claude":
		hook, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": guard, "timeout": 5}}}}}})
		arguments = []string{"--agent", "maestro", "--tools", "Read,Glob,Grep,StructuredOutput", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--settings", string(hook), "--disable-slash-commands", "--output-format", "json", "--json-schema", schema, "--add-dir", provider.Project, "--add-dir", provider.Directory, "--print", prompt}
	case "antigravity":
		arguments = []string{"--agent", "maestro", "--mode", "plan", "--output-format", "json", "--json-schema", schema, "--add-dir", provider.Project, "--add-dir", provider.Directory, "--print-timeout", "0", "--print", prompt}
	default:
		return nil, fmt.Errorf("unsupported Maestro provider %q", provider.Name)
	}
	if provider.Name == "codex" {
		arguments = append([]string{"--no-daemon"}, arguments...)
	}
	if provider.Evidence != "" && provider.Name != "codex" {
		arguments = append([]string{"--add-dir", provider.Evidence}, arguments...)
	}
	if provider.Name != "codex" {
		for _, root := range provider.ReadRoots {
			arguments = append([]string{"--add-dir", root}, arguments...)
		}
	}
	bubblewrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("Maestro requires Bubblewrap for read-only provider isolation; install bubblewrap and retry")
	}
	wrapped := []string{"--die-with-parent", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--bind", directory, directory, "--chdir", directory}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	// Providers need their own authentication/session storage, never project write access.
	stateDirectory := map[string]string{"codex": ".codex", "claude": ".claude", "antigravity": ".gemini"}[provider.Name]
	state := filepath.Join(home, stateDirectory)
	if info, err := os.Stat(state); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s Maestro profile is not installed; run its setup-agents tool", provider.Name)
	}
	if provider.Name == "antigravity" {
		if err := ensureAntigravityGuard(home, provider.Self); err != nil {
			return nil, fmt.Errorf("install scoped Antigravity read-only guard: %w", err)
		}
	}
	wrapped = append(wrapped, "--bind", state, state)
	if provider.Name == "claude" {
		path := filepath.Join(home, ".claude.json")
		if _, err := os.Stat(path); err == nil {
			wrapped = append(wrapped, "--bind", path, path)
		}
	}
	wrapped = append(wrapped, "--", provider.Binary)
	wrapped = append(wrapped, arguments...)
	command := exec.CommandContext(ctx, bubblewrap, wrapped...)
	command.Dir = directory
	command.Env = append(command.Environ(), "TB_MAESTRO_RESTRICTED=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	stdout, err := os.OpenFile(filepath.Join(directory, "stdout.log"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(directory, "stderr.log"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer stderr.Close()
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("%s %s failed: %w; inspect %s", provider.Name, phase, err, directory)
	}
	if provider.Name == "codex" {
		data, err := os.ReadFile(resultPath)
		if err != nil {
			return nil, fmt.Errorf("Codex exited without its structured final result: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(filepath.Join(directory, "stdout.log"))
	if err != nil {
		return nil, err
	}
	return structuredResult(provider.Name, data)
}

func structuredResult(provider string, data []byte) ([]byte, error) {
	var envelope struct {
		Type       string          `json:"type"`
		Subtype    string          `json:"subtype"`
		IsError    bool            `json:"is_error"`
		Status     string          `json:"status"`
		Error      any             `json:"error"`
		Structured json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("invalid %s completion envelope: %w", provider, err)
	}
	if (provider == "claude" && (envelope.Type != "result" || envelope.Subtype != "success" || envelope.IsError)) || (provider == "antigravity" && envelope.Status != "SUCCESS") {
		return nil, fmt.Errorf("%s did not report success: %s %s %v", provider, envelope.Subtype, envelope.Status, envelope.Error)
	}
	if len(envelope.Structured) == 0 || string(envelope.Structured) == "null" {
		return nil, fmt.Errorf("%s completed without structured output", provider)
	}
	return envelope.Structured, nil
}

// Guard denies every provider tool except explicit read-only context and native readers.
func Guard(input io.Reader, output io.Writer) error {
	return GuardForProvider(input, output, "")
}

// GuardForProvider translates the same fail-closed read-only policy into a provider's hook protocol.
func GuardForProvider(input io.Reader, output io.Writer, provider string) error {
	var message struct {
		ToolName string `json:"tool_name"`
		ToolCall struct {
			Name string `json:"name"`
		} `json:"toolCall"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 1024*1024)).Decode(&message); err != nil {
		return err
	}
	if provider == "antigravity" {
		allowed := map[string]bool{"view_file": true, "list_dir": true, "grep_search": true, "find_by_name": true}
		decision := "deny"
		if allowed[message.ToolCall.Name] || workerToolAllowed(message.ToolCall.Name) {
			decision = "allow"
		}
		return json.NewEncoder(output).Encode(map[string]any{"decision": decision, "reason": "Maestro planning and review may only read project context. Toolbox owns implementation and tests."})
	}
	allowed := map[string]bool{"Read": true, "Glob": true, "Grep": true, "StructuredOutput": true, "mcp__maestro_context__read_file": true, "mcp__maestro_context__list_files": true, "mcp__maestro_context__search": true}
	if allowed[message.ToolName] || workerToolAllowed(message.ToolName) {
		return json.NewEncoder(output).Encode(map[string]any{})
	}
	return json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "Maestro planning and review are read-only. Toolbox owns OpenCode execution and verification."}})
}

func workerToolAllowed(name string) bool {
	for _, tool := range []string{"read_file", "list_files", "search", "start_implementation", "job_status", "wait_for_job", "job_logs", "review_job"} {
		if name == "mcp__maestro_worker__"+tool {
			return true
		}
	}
	return false
}

func ensureAntigravityGuard(home, self string) error {
	directory := filepath.Join(home, ".gemini", "config")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	path := filepath.Join(directory, "hooks.json")
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
			return fmt.Errorf("existing Antigravity hooks.json must be a JSON object")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	command := `if [ "${TB_MAESTRO_RESTRICTED:-}" = 1 ]; then exec ` + shellQuote(self) + ` maestro-guard --provider antigravity; else cat >/dev/null; printf '%s\n' '{"decision":"allow"}'; fi`
	settings["toolbox-maestro"] = map[string]any{"PreToolUse": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 5}}}}}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".maestro-hooks-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Request defines one OpenCode implementation task.
type Request struct {
	Project              string
	Task                 string
	Criteria             string
	Model                string
	Verify               string
	ContextLength        int
	OnVerifying          func() error `json:"-"`
	ReadOnlyVerification bool
	Artifacts            []Artifact
	AllowedArtifactRoots []string
}

// Artifact declares a file the supervisor must observe independently of shell test claims.
type Artifact struct {
	Path       string `json:"path"`
	Executable bool   `json:"executable"`
}

// BuildArguments validates a request and returns an argument-safe OpenCode invocation.
func BuildArguments(request Request) ([]string, error) {
	if strings.TrimSpace(request.Project) == "" {
		return nil, fmt.Errorf("project directory is required")
	}
	if strings.TrimSpace(request.Task) == "" {
		return nil, fmt.Errorf("task is required")
	}
	if strings.TrimSpace(request.Criteria) == "" {
		return nil, fmt.Errorf("acceptance criteria are required")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, fmt.Errorf("select an installed local model before running a worker")
	}
	if strings.TrimSpace(request.Verify) == "" {
		return nil, fmt.Errorf("verification command is required; pass --verify with bounded acceptance checks that fail when the requested deliverable is missing or invalid")
	}
	if request.ContextLength != 16384 && request.ContextLength != 32768 {
		return nil, fmt.Errorf("worker context length must match the configured Ollama window (16384 or 32768)")
	}
	prompt := fmt.Sprintf("Implement: %s\n\nAcceptance criteria: %s\n\nCreate or modify the actual files using tools. An answer containing code is not implementation. Use ASCII quotes in source code, never typographic quotes. Never use pkill or killall: filename patterns can terminate OpenCode itself because its command line contains the task. Use timeout -k for bounded runtime checks. Run relevant tests and fix reported runtime errors. Bound all runtime tests with a timeout and a forced kill deadline. Do not launch animations, servers, or other looping programs unbounded or in the background. Keep tool output and the final summary concise. Do not commit or push.\n\nIndependent acceptance command (must pass from the project directory):\n%s", request.Task, request.Criteria, request.Verify)
	return []string{"run", "--standalone", "--agent", "build", "--model", "ollama/" + request.Model, prompt}, nil
}

// Run invokes OpenCode in the requested project and preserves child output.
func Run(ctx context.Context, binary string, request Request, output, errorOutput io.Writer) error {
	arguments, err := BuildArguments(request)
	if err != nil {
		return err
	}
	info, err := os.Stat(request.Project)
	if err != nil {
		return fmt.Errorf("inspect project directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project path is not a directory: %s", request.Project)
	}
	roots := request.AllowedArtifactRoots
	if len(roots) == 0 {
		roots = []string{request.Project}
	}
	if err := ValidateArtifacts(request.Artifacts, roots); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	ownProcessGroup(command)
	command.Dir = request.Project
	settings, err := runtimeConfig(request)
	if err != nil {
		return err
	}
	command.Env = append(command.Environ(), "OPENCODE_CONFIG_CONTENT="+settings)
	if request.ReadOnlyVerification {
		scratch, err := os.MkdirTemp("", "maestro-worker-tests-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(scratch)
		command.Env = append(command.Env, "TMPDIR="+scratch)
	}
	command.Stdout = output
	command.Stderr = errorOutput
	if err := command.Run(); err != nil {
		return fmt.Errorf("run OpenCode worker: %w", err)
	}
	if request.OnVerifying != nil {
		if err := request.OnVerifying(); err != nil {
			return fmt.Errorf("record verification state: %w", err)
		}
	}
	for _, artifact := range request.Artifacts {
		if err := ValidateArtifacts([]Artifact{artifact}, roots); err != nil {
			return err
		}
		if !filepath.IsAbs(artifact.Path) {
			return fmt.Errorf("declared artifact must use an absolute path: %s", artifact.Path)
		}
		if err := checkArtifact(artifact, roots); err != nil {
			return err
		}
	}
	verificationContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	check := exec.CommandContext(verificationContext, "/bin/sh", "-c", request.Verify)
	if request.ReadOnlyVerification {
		bubblewrap, err := exec.LookPath("bwrap")
		if err != nil {
			return fmt.Errorf("independent verification requires Bubblewrap; install bubblewrap and retry")
		}
		scratch, err := os.MkdirTemp("", "maestro-verification-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(scratch)
		check = exec.CommandContext(verificationContext, bubblewrap, "--die-with-parent", "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--bind", scratch, scratch, "--setenv", "TMPDIR", scratch, "--setenv", "HOME", scratch, "--chdir", request.Project, "--", "/bin/sh", "-c", request.Verify)
	}
	ownProcessGroup(check)
	check.Dir = request.Project
	check.Stdout = output
	check.Stderr = errorOutput
	check.WaitDelay = time.Second
	if err := check.Run(); err != nil {
		return fmt.Errorf("worker acceptance check failed (%s): %w; task is incomplete, preserve partial changes and stop", request.Verify, err)
	}
	return nil
}

func checkArtifact(artifact Artifact, roots []string) error {
	resolved, err := filepath.EvalSymlinks(artifact.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worker acceptance failed: declared artifact is missing: %s: %w. OpenCode exited without creating the required file. This does not establish a permission or read-only filesystem problem. Stop and report this implementation failure without requesting chmod or a different destination unless a separate permission error was recorded", artifact.Path, err)
		}
		return fmt.Errorf("worker acceptance failed: inspect declared artifact %s: %w", artifact.Path, err)
	}
	for _, root := range roots {
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		// Stable directory/file descriptors prevent a background symlink swap from escaping scope.
		rootFD, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
		if err != nil {
			return fmt.Errorf("open authorized artifact directory without symlink traversal: %w", err)
		}
		fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS})
		unix.Close(rootFD)
		if err != nil {
			return fmt.Errorf("open declared artifact within authorized directory: %w", err)
		}
		file := os.NewFile(uintptr(fd), artifact.Path)
		info, err := file.Stat()
		file.Close()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("worker acceptance failed: declared artifact is not a regular file: %s", artifact.Path)
		}
		if artifact.Executable && info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("worker acceptance failed: declared artifact is not executable: %s", artifact.Path)
		}
		return nil
	}
	return fmt.Errorf("declared artifact is outside authorized directories: %s", artifact.Path)
}

// ValidateArtifacts confines declared files to explicitly authorized, symlink-resolved directories.
func ValidateArtifacts(artifacts []Artifact, roots []string) error {
	for _, artifact := range artifacts {
		if !filepath.IsAbs(artifact.Path) {
			return fmt.Errorf("declared artifact must use an absolute path")
		}
		resolved, err := prospectivePath(artifact.Path)
		if err != nil {
			return err
		}
		allowed := false
		for _, root := range roots {
			canonical, err := filepath.EvalSymlinks(root)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(canonical, resolved)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("declared artifact %s is outside authorized directories; choose the correct --project or explicitly authorize its directory with --allow-write", artifact.Path)
		}
	}
	return nil
}

func prospectivePath(path string) (string, error) {
	path = filepath.Clean(path)
	var missing []string
	for {
		if _, err := os.Lstat(path); err == nil {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("cannot resolve artifact path")
		}
		path = parent
	}
}

func ownProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
}

// runtimeConfig aligns OpenCode's input and output budgets with Ollama's real window.
func runtimeConfig(request Request) (string, error) {
	settings := map[string]any{}
	if raw := os.Getenv("OPENCODE_CONFIG_CONTENT"); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil || settings == nil {
			return "", fmt.Errorf("OPENCODE_CONFIG_CONTENT must be a JSON object")
		}
	}
	object := func(parent map[string]any, key string) map[string]any {
		value, ok := parent[key].(map[string]any)
		if !ok {
			value = map[string]any{}
			parent[key] = value
		}
		return value
	}
	model := object(object(object(settings, "providers"), "ollama"), "models")
	entry := object(model, request.Model)
	entry["limit"] = map[string]int{"context": request.ContextLength, "input": request.ContextLength - 4096, "output": 4096}
	policies, _ := settings["policies"].([]any)
	for _, resource := range []string{"shell:*pkill*", "shell:*killall*"} {
		policies = append(policies, map[string]string{"action": "tool.use", "resource": resource, "effect": "deny"})
	}
	settings["policies"] = policies
	compaction := object(settings, "compaction")
	compaction["auto"] = true
	compaction["buffer"] = 2048
	encoded, err := json.Marshal(settings)
	return string(encoded), err
}

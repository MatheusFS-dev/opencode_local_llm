package maestro

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ContextReader exposes only bounded file reading and searching through MCP stdio.
type ContextReader struct {
	Roots  []string
	Worker *WorkerServer
}

func (reader ContextReader) resolve(path string) (string, error) {
	if len(reader.Roots) == 0 {
		return "", fmt.Errorf("no read-only context roots configured")
	}
	if path == "" {
		path = reader.Roots[0]
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(reader.Roots[0], path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, root := range reader.Roots {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(realRoot, resolved)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path is outside the authorized read-only context roots")
}

// Call executes one of the three read-only context operations.
func (reader ContextReader) Call(name string, arguments json.RawMessage) (string, error) {
	if reader.Worker != nil && name != "read_file" && name != "list_files" && name != "search" {
		return reader.Worker.Call(name, arguments)
	}
	var input struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return "", err
	}
	path, err := reader.resolve(input.Path)
	if err != nil {
		return "", err
	}
	if name == "read_file" {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("context reads require a regular file")
		}
		data, err := io.ReadAll(io.LimitReader(file, 65537))
		if len(data) > 65536 {
			return string(data[:65536]) + "\n[truncated at 64 KiB]", err
		}
		return string(data), err
	}
	var argumentsList []string
	switch name {
	case "list_files":
		argumentsList = []string{"--files", "--hidden", "-g", "!.git", "--", path}
	case "search":
		if strings.TrimSpace(input.Query) == "" {
			return "", fmt.Errorf("a nonempty search query is required")
		}
		argumentsList = []string{"--line-number", "--max-count", "20", "--hidden", "-g", "!.git", "--fixed-strings", "--", input.Query, path}
	default:
		return "", fmt.Errorf("unsupported read-only context tool %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "rg", argumentsList...)
	var output cappedBuffer
	command.Stdout, command.Stderr = &output, &output
	err = command.Run()
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		err = nil
	}
	return output.String(), err
}

type cappedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	length := len(data)
	if room := 65536 - buffer.Len(); room > 0 {
		buffer.Buffer.Write(data[:min(room, length)])
	}
	if buffer.Len() >= 65536 {
		buffer.truncated = true
	}
	return length, nil
}
func (buffer *cappedBuffer) String() string {
	if buffer.truncated {
		return buffer.Buffer.String() + "\n[truncated at 64 KiB]"
	}
	return buffer.Buffer.String()
}

// Serve speaks MCP JSON-RPC on stdio without emitting human-facing text into the protocol.
func (reader ContextReader) Serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		var rpcError any
		switch request.Method {
		case "initialize":
			name := "maestro-context"
			instructions := "Read-only file context. No execution or implementation tools are available."
			if reader.Worker != nil {
				name = "maestro-worker"
				instructions = "Delegate implementation and test execution through start_implementation. Keep waiting while the job is queued, running, or verifying. Silence and individual wait expiration are not failures. Review actual artifacts and test logs only after awaiting_review. Failed jobs require the user to resolve the error, with no direct implementation fallback."
			}
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": name, "version": "1.0.0"}, "instructions": instructions}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			tools := []any{}
			for _, name := range []string{"read_file", "list_files", "search"} {
				properties := map[string]any{"path": map[string]any{"type": "string", "description": "Absolute or project-relative path inside an authorized context root"}}
				required := []string{"path"}
				if name == "search" {
					properties["query"] = map[string]any{"type": "string"}
					required = append(required, "query")
				}
				tools = append(tools, map[string]any{"name": name, "description": "Read-only Maestro context: " + name, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false}})
			}
			if reader.Worker != nil {
				tools = append(tools, workerTools()...)
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			var call struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			err := json.Unmarshal(request.Params, &call)
			text := ""
			if err == nil {
				text, err = reader.Call(call.Name, call.Arguments)
			}
			if err != nil {
				text = err.Error()
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}
		default:
			rpcError = map[string]any{"code": -32601, "message": "unsupported MCP method"}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		if rpcError != nil {
			response["error"] = rpcError
		} else {
			response["result"] = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

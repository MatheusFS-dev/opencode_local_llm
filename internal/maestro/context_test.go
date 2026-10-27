package maestro

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyContextRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	reader := ContextReader{Roots: []string{root}}
	if _, err := reader.Call("read_file", json.RawMessage(`{"path":"link"}`)); err == nil {
		t.Fatal("escaped read scope")
	}
	if _, err := reader.Call("write_file", json.RawMessage(`{"path":"new"}`)); err == nil {
		t.Fatal("context server exposes writing")
	}
}

func TestContextProtocolReturnsOnlyReadToolsAndFileBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "example.txt"), []byte("actual file contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"read_file\",\"arguments\":{\"path\":\"example.txt\"}}}\n")
	var output bytes.Buffer
	if err := (ContextReader{Roots: []string{root}}).Serve(input, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "actual file contents") {
		t.Fatalf("missing file contents: %s", output.String())
	}
	if strings.Contains(output.String(), "write_file") || strings.Contains(output.String(), "run_command") {
		t.Fatal("unsafe tools advertised")
	}
}

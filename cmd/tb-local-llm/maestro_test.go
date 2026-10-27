package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHelperParsesAntigravityProviderFlag(t *testing.T) {
	root := t.TempDir()
	input, err := os.Create(filepath.Join(root, "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString(`{"toolCall":{"name":"view_file"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(root, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousInput, previousOutput := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	defer func() { os.Stdin, os.Stdout = previousInput, previousOutput }()
	if err := runControllerHelper([]string{"maestro-guard", "--provider", "antigravity"}); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.NewDecoder(output).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["decision"] != "allow" {
		t.Fatalf("native AGY read was blocked: %#v", result)
	}
}

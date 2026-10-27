package terminal

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestRuntimeMessagesPrefixEveryLineAndStartOnlyOnce(t *testing.T) {
	var buffer bytes.Buffer
	output := Output{Writer: &buffer, Tool: "local-llm"}
	if err := output.Message("INFO", "Waiting\nStill running"); err != nil {
		t.Fatal(err)
	}
	if err := output.Message("ERROR", "Actual failure"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buffer.String(), "\n") || strings.Contains(buffer.String(), "\n\n") {
		t.Fatalf("startup separator: %q", buffer.String())
	}
	pattern := regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2} local-llm (INFO|ERROR)\] .+$`)
	for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		if !pattern.MatchString(line) {
			t.Fatalf("invalid runtime message: %q", line)
		}
	}
}

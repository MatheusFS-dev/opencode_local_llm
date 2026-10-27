package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// Output formats human-facing local tool messages without altering child program bytes.
type Output struct {
	Writer  io.Writer
	Tool    string
	started bool
}

// Message writes one Toolbox contract message with a single startup separator.
func (output *Output) Message(level, message string) error {
	if !output.started {
		if _, err := fmt.Fprintln(output.Writer); err != nil {
			return err
		}
		output.started = true
	}
	color := ""
	if file, ok := output.Writer.(*os.File); ok && os.Getenv("NO_COLOR") == "" && term.IsTerminal(file.Fd()) {
		color = map[string]string{"PROMPT": "36", "WARN": "33", "ERROR": "31", "SUCCESS": "32", "SKIP": "90"}[level]
	}
	for _, line := range strings.Split(strings.TrimSuffix(message, "\n"), "\n") {
		value := fmt.Sprintf("[%s %s %s] %s", time.Now().Format("15:04:05"), output.Tool, level, line)
		if color != "" {
			value = "\x1b[" + color + "m" + value + "\x1b[0m"
		}
		if _, err := fmt.Fprintln(output.Writer, value); err != nil {
			return err
		}
	}
	return nil
}

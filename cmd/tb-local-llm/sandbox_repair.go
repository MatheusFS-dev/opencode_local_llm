package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/terminal"
)

func repairSandbox(run func(string, ...string) error) error {
	steps := [][]string{
		{"sudo", "apt", "install", "-y", "apparmor-profiles", "apparmor-utils"},
		{"sudo", "install", "-m", "0644", "/usr/share/apparmor/extra-profiles/bwrap-userns-restrict", "/etc/apparmor.d/bwrap-userns-restrict"},
		{"sudo", "apparmor_parser", "-r", "/etc/apparmor.d/bwrap-userns-restrict"},
		{"codex", "--profile", "maestro", "sandbox", "--", "/usr/bin/true"},
	}
	for _, step := range steps {
		if err := run(step[0], step[1:]...); err != nil {
			return fmt.Errorf("%s: %w; stop and resolve this error before retrying the sandbox repair", strings.Join(step, " "), err)
		}
	}
	return nil
}

func runSandboxRepair() error {
	output := terminal.Output{Writer: os.Stdout, Tool: "local-llm"}
	err := func() error {
		release, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return err
		}
		ubuntu := false
		for _, line := range strings.Split(string(release), "\n") {
			if line == "ID=ubuntu" || line == `ID="ubuntu"` {
				ubuntu = true
			}
		}
		if !ubuntu {
			return fmt.Errorf("this repair is for Ubuntu's Bubblewrap AppArmor restriction only")
		}
		for _, name := range []string{"sudo", "apt", "codex", "bwrap"} {
			if _, err := exec.LookPath(name); err != nil {
				return fmt.Errorf("%s is required before repairing the Codex sandbox", name)
			}
		}
		return repairSandbox(func(name string, args ...string) error {
			if err := output.Message("INFO", "Running: "+strings.Join(append([]string{name}, args...), " ")); err != nil {
				return err
			}
			command := exec.Command(name, args...)
			command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
			return command.Run()
		})
	}()
	if err != nil {
		_ = output.Message("ERROR", err.Error())
	} else {
		_ = output.Message("SUCCESS", "Ubuntu sandbox profile loaded and Maestro sandbox verification passed.")
	}
	_ = output.Message("PROMPT", "Press Enter to return to the dashboard.")
	bufio.NewScanner(os.Stdin).Scan()
	return err
}

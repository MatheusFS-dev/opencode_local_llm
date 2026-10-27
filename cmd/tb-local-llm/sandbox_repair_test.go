package main

import (
	"errors"
	"reflect"
	"testing"
)

func TestSandboxRepairRunsTargetedProfileAndNativeVerification(t *testing.T) {
	var calls [][]string
	err := repairSandbox(func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sudo", "apt", "install", "-y", "apparmor-profiles", "apparmor-utils"}, {"sudo", "install", "-m", "0644", "/usr/share/apparmor/extra-profiles/bwrap-userns-restrict", "/etc/apparmor.d/bwrap-userns-restrict"}, {"sudo", "apparmor_parser", "-r", "/etc/apparmor.d/bwrap-userns-restrict"}, {"codex", "--profile", "maestro", "sandbox", "--", "/usr/bin/true"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands: %v", calls)
	}
}

func TestSandboxRepairStopsAfterFailedStep(t *testing.T) {
	count := 0
	err := repairSandbox(func(name string, args ...string) error { count++; return errors.New("permission denied") })
	if err == nil || count != 1 {
		t.Fatalf("continued after failure: %d %v", count, err)
	}
}

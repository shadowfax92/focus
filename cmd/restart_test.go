package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A fake launchctl keeps every restart test outside the owner's launchd
// domain while exercising the real subprocess arguments and error handling.
func runRestart(t *testing.T, installed bool, fail string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "launchctl.log")
	t.Setenv("FOCUS_TEST_LAUNCHCTL_LOG", logPath)
	t.Setenv("FOCUS_TEST_LAUNCHCTL_FAIL", fail)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$FOCUS_TEST_LAUNCHCTL_LOG"
if [ "$1" = "$FOCUS_TEST_LAUNCHCTL_FAIL" ]; then
  echo 'launchd test failure' >&2
  exit 1
fi
`
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if installed {
		if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(plistPath(), []byte("test agent"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command(os.Args[0], "restart")
	command.Env = append(os.Environ(), "FOCUS_TEST_ACTION_PROCESS=1")
	output, err := command.CombinedOutput()
	calls, _ := os.ReadFile(logPath)
	return string(output), string(calls), err
}

func TestRestartUsesKickstart(t *testing.T) {
	output, calls, err := runRestart(t, true, "")
	target := fmt.Sprintf("gui/%d/com.focus.daemon", os.Getuid())
	if err != nil || !strings.Contains(output, "Restarted Focus.") || calls != "print "+target+"\nkickstart -k "+target+"\n" {
		t.Fatalf("output = %q, calls = %q, error = %v", output, calls, err)
	}
}

func TestRestartExplainsMissingService(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprint(installed), func(t *testing.T) {
			output, calls, err := runRestart(t, installed, "print")
			if err == nil || !strings.Contains(output, "focus install") || strings.Contains(calls, "kickstart") {
				t.Fatalf("missing service: output = %q, calls = %q, error = %v", output, calls, err)
			}
		})
	}
}

func TestRestartSurfacesKickstartFailure(t *testing.T) {
	output, _, err := runRestart(t, true, "kickstart")
	if err == nil || !strings.Contains(output, "launchd test failure") || strings.Contains(output, "Restarted Focus.") {
		t.Fatalf("output = %q, error = %v", output, err)
	}
}

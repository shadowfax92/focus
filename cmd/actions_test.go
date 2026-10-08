package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shadowfax92/focus/ipc"
)

// Run each CLI check in a fresh process, as the real binary runs. This keeps
// Cobra's parsed flags from leaking across cases and tests its default output
// streams rather than configuring writers that could conceal regressions.
func TestMain(m *testing.M) {
	if os.Getenv("FOCUS_TEST_ACTION_PROCESS") == "1" {
		rootCmd.SetArgs(os.Args[1:])
		if err := Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runAction exercises Cobra and the JSON socket together without contacting the
// owner's daemon. The short HOME also keeps macOS Unix socket paths in range.
func runAction(t *testing.T, args []string, response ipc.Response) (string, ipc.Request, error) {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "focus-cmd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	listener, err := ipc.Listen()
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan ipc.Request, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = ipc.Serve(listener, func(request ipc.Request) ipc.Response {
			requests <- request
			return response
		})
	}()
	defer func() {
		listener.Close()
		<-stopped
	}()
	command := exec.Command(os.Args[0], args...)
	command.Env = append(os.Environ(), "FOCUS_TEST_ACTION_PROCESS=1")
	var output, errorOutput strings.Builder
	command.Stdout, command.Stderr = &output, &errorOutput
	err = command.Run()
	if err == nil && errorOutput.Len() != 0 {
		t.Errorf("successful CLI command wrote to stderr: %s", errorOutput.String())
	}
	if err != nil && errorOutput.Len() != 0 {
		err = fmt.Errorf("%s", strings.TrimSpace(errorOutput.String()))
	}
	select {
	case request := <-requests:
		return output.String(), request, err
	default:
		return output.String(), ipc.Request{}, err
	}
}

func TestSetOptionalBudget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		budget string
	}{
		{name: "none", args: []string{"set", "Fix setup"}},
		{name: "minutes", args: []string{"set", "Fix setup", "45m"}, budget: "45m"},
		{name: "compound", args: []string{"set", "Write doc", "1h30m"}, budget: "1h30m"},
		{name: "fractional", args: []string{"set", "Quick fix", "500ms"}, budget: "500ms"},
		{name: "literal help text", args: []string{"set", "--", "--help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, request, err := runAction(t, tc.args, ipc.Response{OK: true})
			if err != nil {
				t.Fatal(err)
			}
			text := tc.args[1]
			if text == "--" {
				text = tc.args[2]
			}
			if request.Action != "set" || request.Text != text || request.Budget != tc.budget {
				t.Fatalf("request = %+v, want text %q and budget %q", request, text, tc.budget)
			}
		})
	}
}

func TestSetRejectsInvalidBudgetAndArgCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing text", args: []string{"set"}, want: "arg(s)"},
		{name: "extra argument", args: []string{"set", "Task", "45m", "extra"}, want: "arg(s)"},
		{name: "zero", args: []string{"set", "Task", "0"}, want: "budget must be a positive Go-style duration (for example 45m)"},
		{name: "negative", args: []string{"set", "Task", "-1m"}, want: "budget must be a positive Go-style duration (for example 45m)"},
		{name: "invalid", args: []string{"set", "Task", "tomorrow"}, want: "budget must be a positive Go-style duration (for example 45m)"},
		{name: "empty", args: []string{"set", "Task", ""}, want: "budget must be a positive Go-style duration (for example 45m)"},
		{name: "literal help budget", args: []string{"set", "--", "Task", "--help"}, want: "budget must be a positive Go-style duration (for example 45m)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, request, err := runAction(t, tc.args, ipc.Response{OK: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if request.Action != "" {
				t.Fatalf("invalid command sent a request: %+v", request)
			}
		})
	}
}

func TestSetTrailingHelpDoesNotSendRequest(t *testing.T) {
	for _, args := range [][]string{
		{"set", "Task", "--help"},
		{"set", "Task", "-h"},
		{"set", "Task", "45m", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, request, err := runAction(t, args, ipc.Response{OK: true})
			if err != nil || !strings.Contains(output, "Usage:") || !strings.Contains(output, "set <text> [budget]") {
				t.Fatalf("help output = %q, error = %v", output, err)
			}
			if request.Action != "" {
				t.Fatalf("help sent a request: %+v", request)
			}
		})
	}
}

func TestStatusShowsBudgetAndOverage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		budget  time.Duration
		elapsed int64
		overage time.Duration
		want    string
		absent  string
	}{
		{name: "none", elapsed: 540, absent: "Budget:"},
		{name: "under", budget: 45 * time.Minute, elapsed: 540, want: "Budget:  45m0s\n", absent: "Overage:"},
		{name: "over", budget: 45 * time.Minute, elapsed: 3420, overage: 12 * time.Minute, want: "Budget:  45m0s\nOverage: +12m0s\n"},
		{name: "at budget", budget: 45 * time.Minute, elapsed: 2700, want: "Budget:  45m0s\n", absent: "Overage:"},
		{name: "fractional", budget: 500 * time.Millisecond, elapsed: 1, overage: 500 * time.Millisecond, want: "Budget:  500ms\nOverage: +500ms\n"},
		{name: "fractional before one second", budget: 500 * time.Millisecond, overage: 250 * time.Millisecond, want: "Budget:  500ms\nOverage: +250ms\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, request, err := runAction(t, []string{"status"}, ipc.Response{
				OK: true, Status: &ipc.Status{Text: "Fix setup", Budget: tc.budget, ElapsedSeconds: tc.elapsed, Overage: tc.overage},
			})
			if err != nil {
				t.Fatal(err)
			}
			if request.Action != "status" || !strings.Contains(output, "Focus:   Fix setup\n") || !strings.Contains(output, tc.want) {
				t.Fatalf("status output = %q, want focus and %q; request = %+v", output, tc.want, request)
			}
			if tc.absent != "" && strings.Contains(output, tc.absent) {
				t.Fatalf("status output unexpectedly contains %q: %q", tc.absent, output)
			}
		})
	}
}

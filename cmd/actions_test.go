package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shadowfax92/focus/ipc"
)

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
	var output strings.Builder
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)
	rootCmd.SetArgs(args)
	defer rootCmd.SetArgs(nil)
	err = rootCmd.Execute()
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, request, err := runAction(t, tc.args, ipc.Response{OK: true})
			if err != nil {
				t.Fatal(err)
			}
			if request.Action != "set" || request.Text != tc.args[1] || request.Budget != tc.budget {
				t.Fatalf("request = %+v, want text %q and budget %q", request, tc.args[1], tc.budget)
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

func TestStatusShowsBudgetAndOverage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		budget  time.Duration
		elapsed int64
		want    string
		absent  string
	}{
		{name: "none", elapsed: 540, absent: "Budget:"},
		{name: "under", budget: 45 * time.Minute, elapsed: 540, want: "Budget:  45m0s\n", absent: "Overage:"},
		{name: "over", budget: 45 * time.Minute, elapsed: 3420, want: "Budget:  45m0s\nOverage: +12m0s\n"},
		{name: "at budget", budget: 45 * time.Minute, elapsed: 2700, want: "Budget:  45m0s\n", absent: "Overage:"},
		{name: "fractional", budget: 500 * time.Millisecond, elapsed: 1, want: "Budget:  500ms\nOverage: +500ms\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, request, err := runAction(t, []string{"status"}, ipc.Response{
				OK: true, Status: &ipc.Status{Text: "Fix setup", Budget: tc.budget, ElapsedSeconds: tc.elapsed},
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

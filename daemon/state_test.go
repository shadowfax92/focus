package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveStateOmitsZeroTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.json")
	if err := SaveState(path, State{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"set_at", "reminder_at"} {
		if strings.Contains(string(b), field) {
			t.Fatalf("zero %s should be omitted: %s", field, b)
		}
	}
}

func TestStateBudgetRoundTrip(t *testing.T) {
	for _, budget := range []time.Duration{0, 45 * time.Minute, 90 * time.Minute, 500 * time.Millisecond, time.Nanosecond} {
		t.Run(budget.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "current.json")
			setAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			if err := SaveState(path, State{FocusText: "Fix setup", SetAt: setAt, Budget: budget}); err != nil {
				t.Fatal(err)
			}
			state, err := LoadState(path)
			if err != nil {
				t.Fatal(err)
			}
			if state.FocusText != "Fix setup" || !state.SetAt.Equal(setAt) || state.Budget != budget {
				t.Fatalf("restored state = %+v, want same focus, timestamp, and budget %s", state, budget)
			}
		})
	}
}

func TestLegacyStateHasNoBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "current.json")
	if err := os.WriteFile(path, []byte(`{"focus_text":"Existing task","set_at":"2026-10-08T12:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.FocusText != "Existing task" || state.Budget != 0 {
		t.Fatalf("legacy state = %+v, want existing task without a budget", state)
	}
}

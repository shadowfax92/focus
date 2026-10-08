package ipc

import (
	"os"
	"path/filepath"
	"time"
)

func SocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".focus.sock"
	}
	return filepath.Join(home, ".focus.sock")
}

// Request is one CLI command. Duration is for pause; Budget is the optional
// commitment for a newly set focus, validated again by the daemon.
type Request struct {
	Action   string `json:"action"`
	Text     string `json:"text,omitempty"`
	Duration string `json:"duration,omitempty"`
	Budget   string `json:"budget,omitempty"` // Optional Go-style duration for set.
	Kind     string `json:"kind,omitempty"`
}

// Status is the daemon's current focus snapshot, with wall-clock elapsed time.
type Status struct {
	Text           string     `json:"text,omitempty"`
	SetAt          *time.Time `json:"set_at,omitempty"`
	ElapsedSeconds int64      `json:"elapsed_seconds,omitempty"`
	// Budget uses nanoseconds to preserve any positive Go duration exactly;
	// absence/zero means this focus has no time budget.
	Budget time.Duration `json:"budget_ns,omitempty"`
	// Overage is computed from the precise clock, rather than the legacy
	// whole-second elapsed field, so fractional budgets report crossing too.
	Overage     time.Duration `json:"overage_ns,omitempty"`
	Rung        int           `json:"rung"`
	Paused      bool          `json:"paused"`
	PausedUntil *time.Time    `json:"paused_until,omitempty"`
}

type Response struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Status *Status `json:"status,omitempty"`
}

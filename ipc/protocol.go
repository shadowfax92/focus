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

type Request struct {
	Action   string `json:"action"`
	Text     string `json:"text,omitempty"`
	Duration string `json:"duration,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

type Status struct {
	Text           string     `json:"text,omitempty"`
	SetAt          *time.Time `json:"set_at,omitempty"`
	ElapsedSeconds int64      `json:"elapsed_seconds,omitempty"`
	Rung           int        `json:"rung"`
	Paused         bool       `json:"paused"`
	PausedUntil    *time.Time `json:"paused_until,omitempty"`
}

type Response struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Status *Status `json:"status,omitempty"`
}

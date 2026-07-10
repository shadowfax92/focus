package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shadowfax92/focus/config"
)

type State struct {
	FocusText   string          `json:"focus_text,omitempty"`
	SetAt       time.Time       `json:"set_at,omitempty,omitzero"`
	PausedUntil *time.Time      `json:"paused_until,omitempty"`
	Position    config.Position `json:"position"`
	Machine     MachineState    `json:"machine"`
}

func StatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "state", "focus", "current.json")
	}
	return filepath.Join(home, ".local", "state", "focus", "current.json")
}

func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	if err := json.Unmarshal(b, &state); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".current-*.json")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

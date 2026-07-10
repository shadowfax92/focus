package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	TS       time.Time `json:"ts"`
	Type     string    `json:"type"`
	Kind     string    `json:"kind,omitempty"`
	Text     string    `json:"text,omitempty"`
	Rung     *int      `json:"rung,omitempty"`
	LatencyS *float64  `json:"latency_s,omitempty"`
}

func Rung(n int) *int                  { return &n }
func Latency(seconds float64) *float64 { return &seconds }

type Store struct {
	path string
	mu   sync.Mutex
}

func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "focus", "events.jsonl")
	}
	return filepath.Join(home, ".local", "share", "focus", "events.jsonl")
}

func New(path string) *Store { return &Store{path: path} }
func Default() *Store        { return New(Path()) }

func (s *Store) Append(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.TS.IsZero() {
		event.TS = time.Now()
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create event directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) ReadAll() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("parse event log line %d: %w", line, err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	return events, nil
}

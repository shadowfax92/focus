package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultQuote = "The main thing is to keep the main thing the main thing."

const (
	StyleFullscreen = "fullscreen"
	StylePulse      = "pulse"
)

type Position struct {
	Preset string  `yaml:"preset" json:"preset"`
	X      float64 `yaml:"x" json:"x"`
	Y      float64 `yaml:"y" json:"y"`
}

type Config struct {
	ReminderStyle        string        `yaml:"reminder_style" json:"reminder_style"`
	Interval             time.Duration `yaml:"interval" json:"interval"`
	PulseSeconds         int           `yaml:"pulse_seconds" json:"pulse_seconds"`
	EscalateAfter        int           `yaml:"escalate_after" json:"escalate_after"`
	BreathingGateSeconds int           `yaml:"breathing_gate_seconds" json:"breathing_gate_seconds"`
	IdleOpacity          float64       `yaml:"idle_opacity" json:"idle_opacity"`
	IdlePauseMinutes     int           `yaml:"idle_pause_minutes" json:"idle_pause_minutes"`
	Position             Position      `yaml:"position" json:"position"`
	Quotes               []string      `yaml:"quotes" json:"quotes"`
}

type diskConfig struct {
	ReminderStyle        string   `yaml:"reminder_style"`
	Interval             string   `yaml:"interval"`
	PulseSeconds         int      `yaml:"pulse_seconds"`
	EscalateAfter        int      `yaml:"escalate_after"`
	BreathingGateSeconds int      `yaml:"breathing_gate_seconds"`
	IdleOpacity          float64  `yaml:"idle_opacity"`
	IdlePauseMinutes     int      `yaml:"idle_pause_minutes"`
	Position             Position `yaml:"position"`
	Quotes               []string `yaml:"quotes"`
}

func Default() Config {
	return Config{
		ReminderStyle:        StyleFullscreen,
		Interval:             15 * time.Minute,
		PulseSeconds:         8,
		EscalateAfter:        2,
		BreathingGateSeconds: 3,
		IdleOpacity:          0.30,
		IdlePauseMinutes:     5,
		Position:             Position{Preset: "top-center"},
		Quotes:               []string{defaultQuote},
	}
}

func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "focus", "config.yaml")
	}
	return filepath.Join(home, ".config", "focus", "config.yaml")
}

func Load() (Config, error) { return LoadFrom(Path()) }

func LoadFrom(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	d := toDisk(Default())
	if err := yaml.Unmarshal(b, &d); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg, err := fromDisk(d)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(cfg Config) error { return SaveTo(Path(), cfg) }

func SaveTo(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(toDisk(cfg))
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func Marshal(cfg Config) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(toDisk(cfg))
}

func (c Config) Validate() error {
	switch c.ReminderStyle {
	case StyleFullscreen, StylePulse:
	default:
		return fmt.Errorf("reminder_style must be %s or %s", StyleFullscreen, StylePulse)
	}
	if c.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}
	if c.PulseSeconds < 0 {
		return fmt.Errorf("pulse_seconds must not be negative")
	}
	if c.EscalateAfter < 1 {
		return fmt.Errorf("escalate_after must be at least 1")
	}
	if c.BreathingGateSeconds < 0 {
		return fmt.Errorf("breathing_gate_seconds must not be negative")
	}
	if c.IdleOpacity < 0 || c.IdleOpacity > 1 {
		return fmt.Errorf("idle_opacity must be between 0 and 1")
	}
	if c.IdlePauseMinutes < 0 {
		return fmt.Errorf("idle_pause_minutes must not be negative")
	}
	switch c.Position.Preset {
	case "top-center", "top-right", "top-left", "custom":
	default:
		return fmt.Errorf("position.preset must be top-center, top-right, top-left, or custom")
	}
	return nil
}

func toDisk(c Config) diskConfig {
	return diskConfig{
		ReminderStyle:        c.ReminderStyle,
		Interval:             c.Interval.String(),
		PulseSeconds:         c.PulseSeconds,
		EscalateAfter:        c.EscalateAfter,
		BreathingGateSeconds: c.BreathingGateSeconds,
		IdleOpacity:          c.IdleOpacity,
		IdlePauseMinutes:     c.IdlePauseMinutes,
		Position:             c.Position,
		Quotes:               append([]string(nil), c.Quotes...),
	}
}

func fromDisk(d diskConfig) (Config, error) {
	interval, err := time.ParseDuration(d.Interval)
	if err != nil {
		return Config{}, fmt.Errorf("parse interval: %w", err)
	}
	return Config{
		ReminderStyle:        d.ReminderStyle,
		Interval:             interval,
		PulseSeconds:         d.PulseSeconds,
		EscalateAfter:        d.EscalateAfter,
		BreathingGateSeconds: d.BreathingGateSeconds,
		IdleOpacity:          d.IdleOpacity,
		IdlePauseMinutes:     d.IdlePauseMinutes,
		Position:             d.Position,
		Quotes:               append([]string(nil), d.Quotes...),
	}, nil
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromMergesDefaultsAndParsesDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("interval: 10s\npulse_seconds: 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 10*time.Second || cfg.PulseSeconds != 12 {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
	if cfg.EscalateAfter != 2 || cfg.Position.Preset != "top-center" {
		t.Fatalf("defaults were not preserved: %+v", cfg)
	}
	if cfg.ReminderStyle != StyleFullscreen {
		t.Fatalf("v2 default style not applied: %+v", cfg)
	}
}

func TestReminderStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("reminder_style: pulse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReminderStyle != StylePulse {
		t.Fatalf("reminder_style = %q, want pulse", cfg.ReminderStyle)
	}

	if err := os.WriteFile(path, []byte("reminder_style: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("invalid reminder_style was accepted")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := Default()
	want.Interval = 42 * time.Second
	want.Position = Position{Preset: "custom", X: 12.5, Y: 99}
	want.Quotes = []string{"one", "two"}
	if err := SaveTo(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != want.Interval || got.Position != want.Position || len(got.Quotes) != 2 {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

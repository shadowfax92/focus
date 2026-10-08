package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromMergesDefaultsAndParsesDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("interval: 15m0s\npulse_seconds: 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 15*time.Minute || cfg.PulseSeconds != 12 {
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
	want.PulseInterval = 7 * time.Second
	want.Position = Position{Preset: "custom", X: 12.5, Y: 99}
	want.Quotes = []string{"one", "two"}
	if err := SaveTo(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Interval != want.Interval || got.PulseInterval != want.PulseInterval || got.Position != want.Position || len(got.Quotes) != 2 {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestPulseIntervalConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want time.Duration
	}{
		{"omitted defaults to five minutes", "interval: 15m\n", 5 * time.Minute},
		{"custom duration", "pulse_interval: 2m30s\n", 150 * time.Second},
		{"numeric zero disables", "pulse_interval: 0\n", 0},
		{"quoted zero disables", "pulse_interval: '0'\n", 0},
		{"duration zero disables", "pulse_interval: 0s\n", 0},
		{"equal to interval is valid", "pulse_interval: 15m\n", 15 * time.Minute},
		{"greater than interval is valid", "pulse_interval: 30m\n", 30 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PulseInterval != tc.want {
				t.Fatalf("pulse_interval = %s, want %s", cfg.PulseInterval, tc.want)
			}
			// focus config uses this same serializer; the key must remain
			// visible even when zero disables the fullscreen glows.
			output, err := Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "pulse_interval: "+tc.want.String()+"\n") {
				t.Fatalf("resolved config omitted pulse_interval: %s", output)
			}
		})
	}
}

func TestPulseIntervalRejectsInvalidDurations(t *testing.T) {
	for _, value := range []string{"-1m", "not-a-duration", "5", "''"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("pulse_interval: "+value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFrom(path); err == nil || !strings.Contains(err.Error(), "pulse_interval") {
				t.Fatalf("invalid pulse_interval %q returned %v", value, err)
			}
		})
	}
	bad := Default()
	bad.PulseInterval = -time.Second
	if _, err := Marshal(bad); err == nil {
		t.Fatal("resolved config accepted a negative pulse_interval")
	}
}

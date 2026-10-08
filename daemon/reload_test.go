package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/hud"
	"github.com/shadowfax92/focus/ipc"
)

func replaceConfig(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path+".next", []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err != nil {
		t.Fatal(err)
	}
}

func TestConfigReloadAppliesValidEdit(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := d.cfg
	cfg.IdleOpacity = 0.7
	cfg.PulseSeconds = 12
	cfg.BreathingGateSeconds = 5
	cfg.Position = config.Position{Preset: "top-right"}
	cfg.Quotes = []string{"saved quote"}
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	var presented hud.Config
	d.applyConfigHUD = func(cfg hud.Config) { presented = cfg }
	d.reloadConfig(newConfigWatcher(path))
	response := d.Handle(ipc.Request{Action: "config"})
	if !response.OK || response.Config == nil || response.Config.IdleOpacity != 0.7 || response.Config.Quotes[0] != "saved quote" {
		t.Fatalf("running config = %+v", response)
	}
	if presented.IdleOpacity != 0.7 || presented.Position.Preset != "top-right" || presented.PulseSeconds != 12 || presented.BreathingGate != 5*time.Second {
		t.Fatalf("HUD config = %+v", presented)
	}
	wantEventTypes(t, d, []string{"set"})
}

func TestConfigReloadShortensCurrentReminderWindows(t *testing.T) {
	for _, elapsed := range []time.Duration{time.Minute, 4 * time.Minute} {
		t.Run(elapsed.String(), func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			now = start.Add(elapsed)
			cfg := d.cfg
			cfg.Interval, cfg.PulseInterval = 10*time.Minute, 2*time.Minute
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := config.SaveTo(path, cfg); err != nil {
				t.Fatal(err)
			}
			d.reloadConfig(newConfigWatcher(path))
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set"}) // The save itself never reminds.
			if elapsed == time.Minute {
				now = start.Add(2 * time.Minute)
			} else {
				now = now.Add(time.Second) // Already elapsed: defer one scheduler beat.
			}
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse"})
			now = start.Add(10 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse", "checkin"})
		})
	}
}

func TestConfigReloadRejectsInvalidEditAndClearsErrorAfterRecovery(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	old := d.cfg
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.SaveTo(path, old); err != nil {
		t.Fatal(err)
	}
	watcher := newConfigWatcher(path)
	d.reloadConfig(watcher)
	for _, text := range []string{"interval: 0s\n", "quotes: [\n"} {
		replaceConfig(t, path, text)
		d.reloadConfig(watcher)
		d.reloadConfig(watcher) // An unchanged bad file must retain its error.
		response := d.Handle(ipc.Request{Action: "config"})
		if response.Config == nil || !reflect.DeepEqual(*response.Config, old) || response.ConfigError == "" {
			t.Fatalf("invalid edit replaced config or lost error: %+v", response)
		}
		if status := d.Handle(ipc.Request{Action: "status"}).Status; status == nil || status.ConfigError == "" {
			t.Fatalf("status did not surface reload error: %+v", status)
		}
	}
	replaceConfig(t, path, "interval: 10m\n")
	d.reloadConfig(watcher)
	response := d.Handle(ipc.Request{Action: "config"})
	if response.Config.Interval != 10*time.Minute || response.ConfigError != "" || d.status().ConfigError != "" {
		t.Fatalf("valid save did not recover: %+v", response)
	}
	wantEventTypes(t, d, []string{"set"})
}

func TestConfigReloadDoesNotImmediatelyShowAnOverdueCheckin(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := start
	d := fullscreenPulseDaemon(t, &now)
	now = start.Add(6 * time.Minute)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "interval: 3m\npulse_interval: 0\n")
	d.reloadConfig(newConfigWatcher(path))
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set"})
	now = now.Add(time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin"})
}

func TestConfigReloadDisablesGlowsImmediately(t *testing.T) {
	for _, pulseInterval := range []time.Duration{0, 15 * time.Minute, 30 * time.Minute} {
		t.Run(pulseInterval.String(), func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			now = start.Add(5 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			stopped := false
			d.stopPulseHUD = func() { stopped = true }
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := d.cfg
			cfg.PulseInterval = pulseInterval
			if err := config.SaveTo(path, cfg); err != nil {
				t.Fatal(err)
			}
			d.reloadConfig(newConfigWatcher(path))
			if !stopped || !d.nextPulse.IsZero() {
				t.Fatal("disabling nudges did not stop the active glow and its cadence")
			}
			if response := d.Handle(ipc.Request{Action: "ack"}); response.OK {
				t.Fatal("disabled glow still accepted an acknowledgement")
			}
			now = start.Add(10 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse"})
			now = start.Add(15 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse", "checkin"})
		})
	}
}

func TestConfigReloadQuoteOnlyLeavesDeadlinesAlone(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := start
	d := fullscreenPulseDaemon(t, &now)
	now = start.Add(5 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	now = start.Add(7 * time.Minute)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := d.cfg
	cfg.Quotes = []string{"edited quote"}
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	d.reloadConfig(newConfigWatcher(path))
	if !d.nextTick.Equal(start.Add(15*time.Minute)) || !d.nextPulse.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("quote edit moved deadlines: check-in %v, glow %v", d.nextTick, d.nextPulse)
	}
	now = start.Add(10 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "pulse", "pulse"})
	if content := d.takeoverContentLocked(now); content.Quote != "edited quote" {
		t.Fatalf("next takeover retained old quote: %+v", content)
	}
}

func TestConfigReloadDetectsRenameWithSameMtimeAndSize(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "interval: 15m\n")
	watcher := newConfigWatcher(path)
	d.reloadConfig(watcher)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".next", []byte("interval: 10m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path+".next", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err != nil {
		t.Fatal(err)
	}
	d.reloadConfig(watcher)
	if d.Handle(ipc.Request{Action: "config"}).Config.Interval != 10*time.Minute {
		t.Fatal("atomic replacement with identical timestamp and size was missed")
	}
}

func TestConfigReloadRetriesWhenFileChangesDuringLoad(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "interval: 10m\n")
	watcher := newConfigWatcher(path)
	watcher.load = func(path string) (config.Config, error) {
		cfg, err := config.LoadFrom(path)
		replaceConfig(t, path, "interval: 12m\n")
		return cfg, err
	}
	d.reloadConfig(watcher)
	if d.cfg.Interval != 15*time.Minute || d.status().ConfigError != "" {
		t.Fatal("save racing validation published a stale config/error")
	}
	watcher.load = config.LoadFrom
	d.reloadConfig(watcher)
	if d.cfg.Interval != 12*time.Minute {
		t.Fatal("new file was not retried after a racing save")
	}
}

func TestConfigReloadRecoversFromStatFailure(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "interval: 10m\n")
	watcher := newConfigWatcher(path)
	d.reloadConfig(watcher)
	watcher.stat = func(string) (os.FileInfo, error) { return nil, errors.New("permission denied") }
	d.reloadConfig(watcher)
	if d.cfg.Interval != 10*time.Minute || !strings.Contains(d.status().ConfigError, "permission denied") {
		t.Fatal("stat error did not preserve last good config and surface failure")
	}
	watcher.stat = os.Stat
	d.reloadConfig(watcher)
	if d.status().ConfigError != "" {
		t.Fatal("recovered stat on unchanged file did not clear the error")
	}
}

func TestConfigReloadPreservesDraggedPosition(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	d.moved(123, 456)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "position:\n  preset: top-right\n")
	var position hud.Position
	d.applyConfigHUD = func(cfg hud.Config) { position = cfg.Position }
	d.reloadConfig(newConfigWatcher(path))
	if position != (hud.Position{Preset: "custom", X: 123, Y: 456}) {
		t.Fatalf("reload replaced dragged position: %+v", position)
	}
	restarted, err := LoadState(d.statePath)
	if err != nil || restarted.Position != d.state.Position {
		t.Fatalf("dragged position lost on restart: %+v, %v", restarted.Position, err)
	}
}

func TestConfigReloadSwitchesStyleAtNextReminder(t *testing.T) {
	for _, initialStyle := range []string{config.StyleFullscreen, config.StylePulse} {
		t.Run(initialStyle, func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			now := start
			d := testDaemon(t, &now, initialStyle)
			if err := d.set("switch style", 0); err != nil {
				t.Fatal(err)
			}
			before := eventTypes(t, d)
			cfg := d.cfg
			cfg.ReminderStyle = config.StylePulse
			want := "pulse"
			if initialStyle == config.StylePulse {
				cfg.ReminderStyle, want = config.StyleFullscreen, "checkin"
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := config.SaveTo(path, cfg); err != nil {
				t.Fatal(err)
			}
			now = start.Add(30 * time.Second)
			d.reloadConfig(newConfigWatcher(path))
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, before)
			state, err := LoadState(d.statePath)
			if err != nil || state.Machine.AwaitingAck {
				t.Fatalf("retired pulse survives in restart state: %+v, %v", state.Machine, err)
			}
			now = start.Add(time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, append(before, want))
		})
	}
}

func TestConfigReloadKeepsActiveGlowExpiry(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	now := start
	d := fullscreenPulseDaemon(t, &now)
	now = start.Add(5 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := d.cfg
	cfg.PulseSeconds = 60
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	d.reloadConfig(newConfigWatcher(path))
	now = now.Add(9 * time.Second)
	if response := d.Handle(ipc.Request{Action: "ack"}); response.OK {
		t.Fatal("duration edit extended the ack window beyond the visible glow")
	}
	now = start.Add(10 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if response := d.Handle(ipc.Request{Action: "ack"}); !response.OK {
		t.Fatalf("next glow did not use new duration: %s", response.Error)
	}
}

func TestConfigReloadPreservesOpenTakeover(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	now = now.Add(15 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := d.cfg
	cfg.ReminderStyle = config.StylePulse
	if err := config.SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	d.reloadConfig(newConfigWatcher(path))
	if !d.machine.State().InTakeover {
		t.Fatal("style edit dismissed unanswered takeover")
	}
	if response := d.Handle(ipc.Request{Action: "ack"}); !response.OK {
		t.Fatalf("style edit invalidated takeover acknowledgement: %s", response.Error)
	}
	wantEventTypes(t, d, []string{"set", "checkin", "ack"})
}

func TestConfigReloadIdleThresholdDoesNotFabricateReturn(t *testing.T) {
	for _, threshold := range []int{0, 2, 10} {
		t.Run((time.Duration(threshold) * time.Minute).String(), func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			now = start.Add(6 * time.Minute)
			idle := (6 * time.Minute).Seconds()
			d.idle = func() float64 { return idle }
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := d.cfg
			cfg.IdlePauseMinutes = threshold
			if err := config.SaveTo(path, cfg); err != nil {
				t.Fatal(err)
			}
			d.reloadConfig(newConfigWatcher(path))
			if !d.nextTick.Equal(start.Add(15*time.Minute)) || !d.nextPulse.Equal(start.Add(5*time.Minute)) {
				t.Fatal("idle policy edit changed cadence deadlines")
			}
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set"})
			now = now.Add(time.Second)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			if threshold == 2 {
				wantEventTypes(t, d, []string{"set"}) // Still guarded under the new policy.
				idle = 0
				now = now.Add(time.Second)
				if err := d.poll(); err != nil {
					t.Fatal(err)
				}
				wantEventTypes(t, d, []string{"set", "idle_return", "checkin"})
			} else {
				// A previously suppressed glow is now allowed, after the grace
				// beat, without manufacturing a welcome-back screen/history.
				wantEventTypes(t, d, []string{"set", "pulse"})
			}
		})
	}
}

func TestConfigReloadRetriesTransientReadFailure(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	d := fullscreenPulseDaemon(t, &now)
	path := filepath.Join(t.TempDir(), "config.yaml")
	replaceConfig(t, path, "interval: 15m\n")
	watcher := newConfigWatcher(path)
	d.reloadConfig(watcher)
	replaceConfig(t, path, "interval: 10m\n")
	watcher.load = func(path string) (config.Config, error) {
		return config.Config{}, &os.PathError{Op: "open", Path: path, Err: syscall.EMFILE}
	}
	d.reloadConfig(watcher)
	if d.cfg.Interval != 15*time.Minute || d.status().ConfigError == "" {
		t.Fatal("transient read failure lost the previous config or its error")
	}
	watcher.load = config.LoadFrom
	d.reloadConfig(watcher)
	response := d.Handle(ipc.Request{Action: "config"})
	if response.Config.Interval != 10*time.Minute || response.ConfigError != "" {
		t.Fatalf("read recovery needed another save: %+v", response)
	}
}

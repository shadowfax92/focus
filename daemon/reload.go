package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/hud"
)

const configPollInterval = 2 * time.Second

func passiveCadence(cfg config.Config) bool {
	return cfg.ReminderStyle == config.StyleFullscreen && cfg.PulseInterval > 0 && cfg.PulseInterval < cfg.Interval
}

// reloadedDeadline retains time already spent in this reminder window. An
// elapsed shorter cadence becomes due one scheduler beat after publication,
// never during the save itself. Only the scheduler presents reminders.
func reloadedDeadline(origin time.Time, interval time.Duration, now time.Time) time.Time {
	deadline := origin.Add(interval)
	if !deadline.After(now) {
		return now.Add(schedulerPollInterval)
	}
	return deadline
}

func (d *Daemon) rescheduleConfigLocked(cfg config.Config, now time.Time) {
	old := d.cfg
	if cfg.Interval != old.Interval {
		d.nextTick = reloadedDeadline(d.tickWindow, cfg.Interval, now)
	}
	wasPassive, isPassive := passiveCadence(old), passiveCadence(cfg)
	if !isPassive {
		d.nextPulse = time.Time{}
	} else if !wasPassive {
		// Enabling nudges starts their own new window; changing the primary
		// style must not restart the existing check-in/ladder deadline.
		d.pulseWindow = now
		d.nextPulse = now.Add(cfg.PulseInterval)
	} else if cfg.PulseInterval != old.PulseInterval {
		d.nextPulse = reloadedDeadline(d.pulseWindow, cfg.PulseInterval, now)
	}
	stopGlow := wasPassive && !isPassive
	if cfg.ReminderStyle != old.ReminderStyle && !d.machine.State().InTakeover {
		// A ladder pulse cannot become a passive nudge awaiting a mandatory
		// ack. Preserve an open takeover until answered, but retire a ladder
		// pulse so the next reminder belongs entirely to the new style.
		d.machine.Reset()
		stopGlow = true
	}
	if stopGlow {
		d.passivePulseAt = time.Time{}
		if d.stopPulseHUD != nil {
			d.stopPulseHUD()
		} else {
			hud.StopPulse()
		}
	}
	if cfg.IdlePauseMinutes != old.IdlePauseMinutes && d.idleGuarded {
		// Reclassifying an idle stretch is not user activity. Forget the old
		// threshold's latch and let the next poll evaluate the new policy;
		// overdue glows/check-ins wait a beat rather than firing on the save.
		d.idleGuarded = false
		d.reloadGraceUntil = now.Add(schedulerPollInterval)
	}
}

// configWatcher owns file observation on one goroutine. Tracking identity as
// well as mtime/size catches editors that replace the path with a renamed file.
// The loader/stat seams allow deterministic checks without sleeping in tests.
type configWatcher struct {
	path        string
	stat        func(string) (os.FileInfo, error)
	load        func(string) (config.Config, error)
	last        os.FileInfo
	initialized bool
}

func newConfigWatcher(path string) *configWatcher {
	return &configWatcher{path: path, stat: os.Stat, load: config.LoadFrom}
}

func sameConfigFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

func (w *configWatcher) fileInfo() (os.FileInfo, error) {
	info, err := w.stat(w.path)
	if errors.Is(err, os.ErrNotExist) {
		// Match startup: an absent config resolves to defaults.
		return nil, nil
	}
	return info, err
}

func (w *configWatcher) poll() (config.Config, bool, error) {
	info, err := w.fileInfo()
	if err != nil {
		w.initialized = false // Retry after a transient stat/permission failure.
		return config.Config{}, false, fmt.Errorf("stat config: %w", err)
	}
	if w.initialized && sameConfigFile(w.last, info) {
		return config.Config{}, false, nil
	}
	cfg, loadErr := w.load(w.path)
	after, err := w.fileInfo()
	if err != nil {
		w.initialized = false
		return config.Config{}, false, fmt.Errorf("stat config after read: %w", err)
	}
	if !sameConfigFile(info, after) {
		// A save raced the read. Publish neither a stale config nor a transient
		// parse error, and retry the new identity on the next poll.
		return config.Config{}, false, nil
	}
	// Only successful loads consume the fingerprint. A transient read error
	// (e.g. descriptor exhaustion) can recover without another file edit.
	// Invalid files are retried too; the daemon logs only error transitions.
	w.last, w.initialized = after, loadErr == nil
	return cfg, true, loadErr
}

func (d *Daemon) watchConfig(ctx context.Context, watcher *configWatcher) {
	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()
	// Re-read once even without a metadata change: the startup load may have
	// preceded an editor save, before this watcher observed its first stat.
	d.reloadConfig(watcher)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reloadConfig(watcher)
		}
	}
}

func (d *Daemon) reloadConfig(watcher *configWatcher) {
	// File I/O and validation do not hold the state mutex. Only publication
	// shares the lock used by IPC, reminder scheduling, and drag callbacks.
	cfg, changed, err := watcher.poll()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		if d.configError != err.Error() {
			log.Printf("focus config error: %v (running with previous config)", err)
		}
		d.configError = err.Error()
		return
	}
	if !changed {
		return
	}
	styleChanged := cfg.ReminderStyle != d.cfg.ReminderStyle
	d.rescheduleConfigLocked(cfg, d.now())
	d.cfg = cfg
	d.configError = ""
	if d.state.Position.Preset != "custom" {
		d.state.Position = cfg.Position
	}
	if d.applyConfigHUD != nil {
		d.applyConfigHUD(d.hudConfigLocked())
	} else {
		hud.ApplyConfig(d.hudConfigLocked())
	}
	if styleChanged {
		// Retiring a ladder pulse also retires its persisted ack/rung state.
		// An immediate fallback restart must not resurrect that old reminder.
		if err := d.saveLocked(); err != nil {
			log.Printf("focus save reminder style: %v", err)
		}
	}
	log.Printf("focus config reloaded")
}

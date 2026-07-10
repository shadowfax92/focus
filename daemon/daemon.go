package daemon

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/hud"
	"github.com/shadowfax92/focus/ipc"
	"github.com/shadowfax92/focus/store"
)

type Daemon struct {
	mu          sync.Mutex
	cfg         config.Config
	events      *store.Store
	statePath   string
	state       State
	machine     *Machine
	now         func() time.Time
	idle        func() float64
	nextTick    time.Time
	idleGuarded bool
}

func New(cfg config.Config) (*Daemon, error) {
	state, err := LoadState(StatePath())
	if err != nil {
		return nil, err
	}
	if state.Position.Preset != "custom" {
		state.Position = cfg.Position
	}
	d := &Daemon{
		cfg:       cfg,
		events:    store.Default(),
		statePath: StatePath(),
		state:     state,
		machine:   NewMachine(state.Machine),
		now:       time.Now,
		idle:      idleSeconds,
	}
	d.nextTick = d.now().Add(cfg.Interval)
	return d, nil
}

func Run(cfg config.Config) error {
	d, err := New(cfg)
	if err != nil {
		return err
	}
	listener, err := ipc.Listen()
	if err != nil {
		return err
	}
	go func() {
		if err := ipc.Serve(listener, d.Handle); err != nil {
			log.Printf("focus IPC server stopped: %v", err)
		}
	}()
	go d.loop(context.Background())
	go d.restoreHUD()

	hud.Run(d.hudConfig(), hud.Events{
		OnAck: func(kind hud.AckKind, rung int, latency time.Duration, newText string) {
			go func() {
				if err := d.ack(kind.String(), newText, &latency, &rung); err != nil {
					log.Printf("focus HUD ack: %v", err)
				}
			}()
		},
		OnMoved: func(x, y float64) { go d.moved(x, y) },
	})
	return nil
}

func (d *Daemon) hudConfig() hud.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return hud.Config{
		IdleOpacity: d.cfg.IdleOpacity,
		Position: hud.Position{
			Preset: d.state.Position.Preset,
			X:      d.state.Position.X,
			Y:      d.state.Position.Y,
		},
		BreathingGate: time.Duration(d.cfg.BreathingGateSeconds) * time.Second,
		PulseSeconds:  d.cfg.PulseSeconds,
	}
}

// fullscreen reports whether reminders go straight to the check-in screen.
// In this mode the pill is never shown: nothing is on screen between
// reminders, so SetFocus/ClearFocus/Pulse are only driven in pulse style.
func (d *Daemon) fullscreen() bool { return d.cfg.ReminderStyle != config.StylePulse }

// reminderLocked is the immediate reminder fired outside the tick cadence
// (welcome-back after idle, or on-set in pulse style).
func (d *Daemon) reminderLocked(now time.Time) Action {
	if d.fullscreen() {
		return d.machine.Checkin(now)
	}
	return d.machine.Start(now)
}

func (d *Daemon) tickLocked(now time.Time) Action {
	if d.fullscreen() {
		return d.machine.Checkin(now)
	}
	return d.machine.Tick(now, d.cfg.EscalateAfter)
}

func (d *Daemon) restoreHUD() {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if d.state.FocusText == "" {
		hud.ClearFocus()
		return
	}
	if !d.fullscreen() {
		hud.SetFocus(d.state.FocusText, d.state.SetAt)
	}
	paused := d.isPaused(now)
	hud.SetPaused(paused)
	if !paused && d.machine.State().InTakeover {
		hud.ShowTakeover(d.takeoverContentLocked(now))
	}
}

func (d *Daemon) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.poll(); err != nil {
				log.Printf("focus scheduler: %v", err)
			}
		}
	}
}

func (d *Daemon) poll() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if d.state.PausedUntil != nil {
		if now.Before(*d.state.PausedUntil) {
			return nil
		}
		if err := d.resumeLocked(now); err != nil {
			return err
		}
	}
	if d.state.FocusText == "" {
		// A guard left armed here would greet the next `focus set` with an
		// instant bogus welcome-back reminder.
		d.idleGuarded = false
		return nil
	}
	if d.cfg.IdlePauseMinutes > 0 {
		threshold := float64(d.cfg.IdlePauseMinutes * 60)
		if d.idle() >= threshold {
			d.idleGuarded = true
			return nil
		}
		if d.idleGuarded {
			d.idleGuarded = false
			if err := d.appendLocked(store.Event{TS: now, Type: "idle_return"}); err != nil {
				return err
			}
			// A reminder is already up from before the idle stretch — leave
			// it; resetting would double-count and restamp its latency.
			if d.machine.State().InTakeover {
				return d.saveLocked()
			}
			d.machine.Reset()
			action := d.reminderLocked(now)
			d.nextTick = now.Add(d.cfg.Interval)
			return d.performLocked(action, now)
		}
	}
	if now.Before(d.nextTick) {
		return nil
	}
	action := d.tickLocked(now)
	d.nextTick = now.Add(d.cfg.Interval)
	return d.performLocked(action, now)
}

func (d *Daemon) Handle(request ipc.Request) ipc.Response {
	switch request.Action {
	case "ping":
		return ipc.Response{OK: true}
	case "set":
		if err := d.set(request.Text); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "done":
		if err := d.done(); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "pause":
		duration, err := time.ParseDuration(request.Duration)
		if err != nil || duration <= 0 {
			return ipc.Response{Error: "pause duration must be a positive Go-style duration (for example 45m)"}
		}
		if err := d.pause(duration); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "resume":
		if err := d.resume(); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "ack":
		if err := d.ack(request.Kind, request.Text, nil, nil); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "status":
		status := d.status()
		return ipc.Response{OK: true, Status: &status}
	default:
		return ipc.Response{Error: "unknown action"}
	}
}

func (d *Daemon) set(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("focus text cannot be empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.state.FocusText = text
	d.state.SetAt = now
	d.machine.Reset()
	d.idleGuarded = false
	d.nextTick = now.Add(d.cfg.Interval)
	if err := d.appendLocked(store.Event{TS: now, Type: "set", Text: text}); err != nil {
		return err
	}
	hud.DismissTakeover()
	if d.fullscreen() {
		// The first check-in comes a full interval out — `focus set` must not
		// answer with an instant screen grab.
		return d.saveLocked()
	}
	hud.SetFocus(text, now)
	if d.isPaused(now) {
		hud.SetPaused(true)
		return d.saveLocked()
	}
	if err := d.performLocked(d.machine.Start(now), now); err != nil {
		return err
	}
	return d.saveLocked()
}

func (d *Daemon) done() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.appendLocked(store.Event{TS: d.now(), Type: "done"}); err != nil {
		return err
	}
	return d.clearFocusLocked()
}

// clearFocusLocked ends the current focus everywhere — state, pause, machine,
// pixels. Nothing fires again until the next set. Both completion paths
// (`focus done` and a done ack with nothing next) go through here so their
// semantics cannot drift.
func (d *Daemon) clearFocusLocked() error {
	d.state.FocusText = ""
	d.state.SetAt = time.Time{}
	d.state.PausedUntil = nil
	d.machine.Reset()
	hud.DismissTakeover()
	hud.ClearFocus()
	return d.saveLocked()
}

func (d *Daemon) pause(duration time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	until := now.Add(duration)
	d.state.PausedUntil = &until
	if err := d.appendLocked(store.Event{TS: now, Type: "pause"}); err != nil {
		return err
	}
	hud.DismissTakeover()
	hud.SetPaused(true)
	return d.saveLocked()
}

func (d *Daemon) resume() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.resumeLocked(d.now())
}

func (d *Daemon) resumeLocked(now time.Time) error {
	if d.state.PausedUntil == nil {
		return nil
	}
	d.state.PausedUntil = nil
	d.nextTick = now.Add(d.cfg.Interval)
	if err := d.appendLocked(store.Event{TS: now, Type: "resume"}); err != nil {
		return err
	}
	hud.SetPaused(false)
	if d.state.FocusText != "" {
		if !d.fullscreen() {
			hud.SetFocus(d.state.FocusText, d.state.SetAt)
		}
		if d.machine.State().InTakeover {
			hud.ShowTakeover(d.takeoverContentLocked(now))
		}
	}
	return d.saveLocked()
}

// ack records the response to a reminder. kind "done" completes the current
// focus: it logs ack + done, then either sets newText as the next focus or —
// when newText is empty — clears everything so no reminder fires until the
// next `focus set`.
func (d *Daemon) ack(kind, newText string, latency *time.Duration, reportedRung *int) error {
	if kind == "" {
		kind = "on_task"
	}
	switch kind {
	case "on_task", "drifted", "refocus", "done":
	default:
		return fmt.Errorf("ack kind must be on_task, drifted, refocus, or done")
	}
	if kind == "refocus" && strings.TrimSpace(newText) == "" {
		return fmt.Errorf("refocus ack requires new focus text")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.FocusText == "" {
		return fmt.Errorf("nothing is currently focused")
	}
	now := d.now()
	previous := d.machine.State()
	rung := previous.Rung
	if reportedRung != nil {
		rung = *reportedRung
	}
	var latencySeconds *float64
	if latency != nil {
		seconds := latency.Seconds()
		latencySeconds = &seconds
	} else if !previous.ReminderAt.IsZero() {
		seconds := now.Sub(previous.ReminderAt).Seconds()
		if seconds >= 0 {
			latencySeconds = &seconds
		}
	}
	if err := d.appendLocked(store.Event{
		TS: now, Type: "ack", Kind: kind, Rung: store.Rung(rung), LatencyS: latencySeconds,
	}); err != nil {
		return err
	}
	d.machine.Ack()
	d.nextTick = now.Add(d.cfg.Interval)
	hud.DismissTakeover()
	newText = strings.TrimSpace(newText)
	if kind == "done" {
		if err := d.appendLocked(store.Event{TS: now, Type: "done"}); err != nil {
			return err
		}
		if newText == "" {
			return d.clearFocusLocked()
		}
	}
	if newText != "" && (kind == "refocus" || kind == "done") {
		d.state.FocusText = newText
		d.state.SetAt = now
		if err := d.appendLocked(store.Event{TS: now, Type: "set", Text: newText}); err != nil {
			return err
		}
		if !d.fullscreen() {
			hud.SetFocus(newText, now)
		}
	}
	return d.saveLocked()
}

func (d *Daemon) status() ipc.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	status := ipc.Status{
		Text:        d.state.FocusText,
		Rung:        d.machine.State().Rung,
		Paused:      d.isPaused(now),
		PausedUntil: d.state.PausedUntil,
	}
	if !d.state.SetAt.IsZero() {
		setAt := d.state.SetAt
		status.SetAt = &setAt
		status.ElapsedSeconds = int64(now.Sub(setAt).Seconds())
	}
	return status
}

func (d *Daemon) performLocked(action Action, now time.Time) error {
	switch action.Kind {
	case ActionNone:
		return nil
	case ActionPulse:
		if err := d.appendLocked(store.Event{TS: now, Type: "pulse", Rung: store.Rung(action.Rung)}); err != nil {
			return err
		}
		hud.Pulse(action.Rung)
	case ActionTakeover:
		if err := d.appendLocked(store.Event{TS: now, Type: "escalation", Rung: store.Rung(action.Rung)}); err != nil {
			return err
		}
		hud.ShowTakeover(d.takeoverContentLocked(now))
	case ActionCheckin:
		if err := d.appendLocked(store.Event{TS: now, Type: "checkin"}); err != nil {
			return err
		}
		hud.ShowTakeover(d.takeoverContentLocked(now))
	}
	return d.saveLocked()
}

// takeoverContentLocked builds the screen for whichever reminder the current
// style shows: a routine check-in (rung 0, keys armed immediately — a
// breathing gate 4×/hour would be pure friction) or a pulse-mode escalation
// (explicit rung, configured gate). Called after the checkin/escalation event
// is appended, so today's count includes the one being shown.
func (d *Daemon) takeoverContentLocked(now time.Time) hud.TakeoverContent {
	quote := ""
	if len(d.cfg.Quotes) > 0 {
		quote = d.cfg.Quotes[rand.IntN(len(d.cfg.Quotes))]
	}
	events, err := d.events.ReadAll()
	if err != nil {
		log.Printf("focus mirror stats: %v", err)
	}
	today := store.DeriveToday(events, now, time.Local)
	minutes := max(int(now.Sub(d.state.SetAt).Minutes()), 0)
	content := hud.TakeoverContent{
		FocusText: d.state.FocusText,
		Quote:     quote,
	}
	if d.fullscreen() {
		// max guards re-shows whose checkin event landed before midnight —
		// "0th check-in today" is worse than an off-by-one at 12:01am.
		content.MirrorLine = fmt.Sprintf("%s check-in today · %dm on task · yesterday: %d distractions",
			ordinal(max(today.Today.Checkins, 1)), minutes, today.Yesterday.Distractions)
		return content
	}
	content.MirrorLine = fmt.Sprintf("%s escalation today · yesterday: %d · %dm on task",
		ordinal(max(today.Today.Escalations, 1)), today.Yesterday.Distractions, minutes)
	content.Rung = d.machine.State().Rung
	content.Gate = time.Duration(d.cfg.BreathingGateSeconds) * time.Second
	return content
}

func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func (d *Daemon) appendLocked(event store.Event) error {
	return d.events.Append(event)
}

func (d *Daemon) saveLocked() error {
	d.state.Machine = d.machine.State()
	return SaveState(d.statePath, d.state)
}

func (d *Daemon) isPaused(now time.Time) bool {
	return d.state.PausedUntil != nil && now.Before(*d.state.PausedUntil)
}

func (d *Daemon) moved(x, y float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Position = config.Position{Preset: "custom", X: x, Y: y}
	if err := d.saveLocked(); err != nil {
		log.Printf("focus save position: %v", err)
	}
}

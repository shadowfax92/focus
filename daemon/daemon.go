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

const schedulerPollInterval = time.Second

// Daemon owns persisted focus state and reminder policy. Its mutex serializes
// IPC, scheduler, config publication, and HUD input before queuing to Cocoa.
// Reminder deadlines and passive glows belong to this process, not persisted state.
type Daemon struct {
	mu          sync.Mutex
	cfg         config.Config
	configError string // Last failed reload; cfg remains the last good snapshot.
	events      *store.Store
	statePath   string
	state       State
	machine     *Machine
	now         func() time.Time
	idle        func() float64
	nextTick    time.Time
	nextPulse   time.Time
	// Window origins survive cadence edits, including a shortened deadline
	// deferred by reload; deriving them from that deadline would drift.
	tickWindow  time.Time
	pulseWindow time.Time
	// An idle-policy edit can release overdue reminders. Give that change a
	// scheduler beat of grace without moving either cadence deadline.
	reloadGraceUntil time.Time
	// Passive glows accept optional clicks only while visible. This timestamp
	// is ephemeral: a daemon restart must not restore a glow as a pending ack.
	passivePulseAt    time.Time
	passivePulseUntil time.Time // The lifetime chosen when this glow was shown.
	passivePulseID    uint64
	idleGuarded       bool
	setFocusHUD       func(string, time.Time, time.Duration)
	setPausedHUD      func(bool)
	applyConfigHUD    func(hud.Config)
	stopPulseHUD      func()
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
	d.resetScheduleLocked(d.now())
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
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Capture startup presentation before the watcher can publish a new config.
	// Later HUD changes are queued to Cocoa after its main-thread initialization.
	initialHUD := d.hudConfig()
	go func() {
		if err := ipc.Serve(listener, d.Handle); err != nil {
			log.Printf("focus IPC server stopped: %v", err)
		}
	}()
	go d.loop(ctx)
	go d.watchConfig(ctx, newConfigWatcher(config.Path()))
	go d.restoreHUD()

	hud.Run(initialHUD, hud.Events{
		OnAck: func(kind hud.AckKind, rung int, latency time.Duration, newText string) {
			go func() {
				if err := d.ack(kind.String(), newText, &latency, &rung, nil); err != nil {
					log.Printf("focus HUD ack: %v", err)
				}
			}()
		},
		OnPassivePulseAck: func(kind hud.AckKind, reminderID uint64, latency time.Duration) {
			// This click may wait behind a scheduler tick. Carry its identity
			// across the goroutine handoff so it cannot answer a newer screen.
			go func() {
				rung := 0
				if err := d.ack(kind.String(), "", &latency, &rung, &reminderID); err != nil {
					log.Printf("focus HUD glow ack: %v", err)
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
	return d.hudConfigLocked()
}

func (d *Daemon) hudConfigLocked() hud.Config {
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

// directCheckins reports whether interval reminders go straight to the
// full-screen check-in instead of climbing the pulse escalation ladder.
// Ambient pill presentation is independent of this cadence choice.
func (d *Daemon) directCheckins() bool { return d.cfg.ReminderStyle != config.StylePulse }

// resetScheduleLocked starts a fresh reminder window after set, resume, or a
// check-in. The passive glow has its own deadline so it cannot delay check-ins
// or feed the pulse-style escalation machine. Zero means no passive cadence.
func (d *Daemon) resetScheduleLocked(now time.Time) {
	d.reloadGraceUntil = time.Time{}
	d.tickWindow, d.pulseWindow = now, now
	d.nextTick = now.Add(d.cfg.Interval)
	d.nextPulse = time.Time{}
	d.passivePulseAt = time.Time{}
	if passiveCadence(d.cfg) {
		d.nextPulse = now.Add(d.cfg.PulseInterval)
	}
}

// presentFocus sends the complete focus to the HUD, including its original
// clock origin and budget on restore/resume. Tests replace this output seam
// so presentation policy can be verified without launching Cocoa.
func (d *Daemon) presentFocus(text string, since time.Time, budget time.Duration) {
	if d.setFocusHUD != nil {
		d.setFocusHUD(text, since, budget)
		return
	}
	hud.SetFocus(text, since, budget)
}

func (d *Daemon) presentPaused(paused bool) {
	if d.setPausedHUD != nil {
		d.setPausedHUD(paused)
		return
	}
	hud.SetPaused(paused)
}

// reminderLocked is the immediate reminder fired outside the tick cadence
// (welcome-back after idle, or on-set in pulse style).
func (d *Daemon) reminderLocked(now time.Time) Action {
	if d.directCheckins() {
		return d.machine.Checkin(now)
	}
	return d.machine.Start(now)
}

func (d *Daemon) tickLocked(now time.Time) Action {
	if d.directCheckins() {
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
	d.presentFocus(d.state.FocusText, d.state.SetAt, d.state.Budget)
	paused := d.isPaused(now)
	d.presentPaused(paused)
	if !paused && d.machine.State().InTakeover {
		hud.ShowTakeover(d.takeoverContentLocked(now))
	}
}

func (d *Daemon) loop(ctx context.Context) {
	ticker := time.NewTicker(schedulerPollInterval)
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
	if now.Before(d.reloadGraceUntil) {
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
			d.resetScheduleLocked(now)
			return d.performLocked(action, now)
		}
	}
	// Check-ins win even if both deadlines became due between polls. Resetting
	// both deadlines also discards the coincident glow instead of replaying it.
	if !now.Before(d.nextTick) {
		action := d.tickLocked(now)
		d.resetScheduleLocked(now)
		return d.performLocked(action, now)
	}
	if !d.nextPulse.IsZero() && !now.Before(d.nextPulse) {
		d.pulseWindow = now
		d.nextPulse = now.Add(d.cfg.PulseInterval)
		if !d.machine.State().InTakeover {
			// Passive rung-0 nudges never enter the acknowledgement ladder.
			return d.performLocked(Action{Kind: ActionPulse}, now)
		}
	}
	return nil
}

func (d *Daemon) Handle(request ipc.Request) ipc.Response {
	switch request.Action {
	case "ping":
		return ipc.Response{OK: true}
	case "set":
		var budget time.Duration
		if request.Budget != "" {
			var err error
			budget, err = time.ParseDuration(request.Budget)
			if err != nil || budget <= 0 {
				return ipc.Response{Error: "budget must be a positive Go-style duration (for example 45m)"}
			}
		}
		if err := d.set(request.Text, budget); err != nil {
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
		if err := d.ack(request.Kind, request.Text, nil, nil, nil); err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true}
	case "status":
		status := d.status()
		return ipc.Response{OK: true, Status: &status}
	case "config":
		d.mu.Lock()
		defer d.mu.Unlock()
		cfg := d.cfg
		cfg.Quotes = append([]string(nil), cfg.Quotes...)
		return ipc.Response{OK: true, Config: &cfg, ConfigError: d.configError}
	default:
		return ipc.Response{Error: "unknown action"}
	}
}

func (d *Daemon) set(text string, budget time.Duration) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("focus text cannot be empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.state.FocusText = text
	d.state.SetAt = now
	d.state.Budget = budget
	d.machine.Reset()
	d.idleGuarded = false
	d.resetScheduleLocked(now)
	if err := d.appendLocked(store.Event{TS: now, Type: "set", Text: text}); err != nil {
		return err
	}
	hud.DismissTakeover()
	d.presentFocus(text, now, budget)
	if d.isPaused(now) {
		d.presentPaused(true)
		return d.saveLocked()
	}
	if d.directCheckins() {
		// The first check-in comes a full interval out — `focus set` must not
		// answer with an instant screen grab.
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
	d.state.Budget = 0
	d.state.PausedUntil = nil
	d.machine.Reset()
	d.passivePulseAt = time.Time{}
	hud.DismissTakeover()
	hud.ClearFocus()
	d.presentPaused(false)
	return d.saveLocked()
}

func (d *Daemon) pause(duration time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	until := now.Add(duration)
	d.state.PausedUntil = &until
	d.passivePulseAt = time.Time{}
	if err := d.appendLocked(store.Event{TS: now, Type: "pause"}); err != nil {
		return err
	}
	hud.DismissTakeover()
	d.presentPaused(true)
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
	d.resetScheduleLocked(now)
	if err := d.appendLocked(store.Event{TS: now, Type: "resume"}); err != nil {
		return err
	}
	d.presentPaused(false)
	if d.state.FocusText != "" {
		d.presentFocus(d.state.FocusText, d.state.SetAt, d.state.Budget)
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
// passivePulseID identifies a specific HUD glow; nil allows CLI and existing
// takeover/ladder acknowledgements to target the currently pending reminder.
func (d *Daemon) ack(kind, newText string, latency *time.Duration, reportedRung *int, passivePulseID *uint64) error {
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
	passive := !previous.AwaitingAck && d.directCheckins() && !d.passivePulseAt.IsZero() &&
		now.Before(d.passivePulseUntil)
	if passivePulseID != nil && (!passive || *passivePulseID != d.passivePulseID) {
		return fmt.Errorf("passive glow acknowledgement is stale")
	}
	if !previous.AwaitingAck && !passive {
		return fmt.Errorf("no reminder is awaiting acknowledgement")
	}
	if passive {
		previous.ReminderAt = d.passivePulseAt
	}
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
	// Optional glow clicks log the response without postponing either timer.
	// Completing/changing the focus still starts a fresh reminder window.
	if !passive || kind == "refocus" || kind == "done" {
		d.resetScheduleLocked(now)
	}
	d.passivePulseAt = time.Time{}
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
		// Inline refocus/next-task input has no budget field. A fresh focus
		// must not inherit the time commitment made for the previous one.
		d.state.Budget = 0
		if err := d.appendLocked(store.Event{TS: now, Type: "set", Text: newText}); err != nil {
			return err
		}
		d.presentFocus(newText, now, 0)
	}
	return d.saveLocked()
}

func (d *Daemon) status() ipc.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	status := ipc.Status{
		ConfigError: d.configError,
		Text:        d.state.FocusText,
		Budget:      d.state.Budget,
		Rung:        d.machine.State().Rung,
		Paused:      d.isPaused(now),
		PausedUntil: d.state.PausedUntil,
	}
	if !d.state.SetAt.IsZero() {
		setAt := d.state.SetAt
		status.SetAt = &setAt
		elapsed := now.Sub(setAt)
		status.ElapsedSeconds = int64(elapsed.Seconds())
		if d.state.Budget > 0 && elapsed > d.state.Budget {
			status.Overage = elapsed - d.state.Budget
		}
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
		if d.directCheckins() {
			// IDs outlive individual glow windows but never the process. A
			// queued click for an expired/replaced glow must not ack its successor.
			d.passivePulseID++
			d.passivePulseAt = now
			d.passivePulseUntil = now.Add(d.passivePulseDuration())
			hud.PassivePulse(d.passivePulseID)
		} else {
			hud.Pulse(action.Rung)
		}
	case ActionTakeover:
		if err := d.appendLocked(store.Event{TS: now, Type: "escalation", Rung: store.Rung(action.Rung)}); err != nil {
			return err
		}
		hud.ShowTakeover(d.takeoverContentLocked(now))
	case ActionCheckin:
		if err := d.appendLocked(store.Event{TS: now, Type: "checkin"}); err != nil {
			return err
		}
		d.passivePulseAt = time.Time{}
		// Dismiss also ends any lingering glow on the UI thread. Queue it
		// before ShowTakeover so long pulse_seconds never overlaps a check-in.
		hud.DismissTakeover()
		hud.ShowTakeover(d.takeoverContentLocked(now))
	}
	return d.saveLocked()
}

// passivePulseDuration matches the existing HUD's rung-0 lifetime, including
// its 8s fallback for pulse_seconds: 0. It bounds optional CLI/pill acks without
// making an ignored nudge part of the persisted escalation machine.
func (d *Daemon) passivePulseDuration() time.Duration {
	seconds := d.cfg.PulseSeconds
	if seconds <= 0 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

// takeoverContentLocked builds the screen for whichever reminder the current
// style shows: a routine check-in (rung 0, no breathing gate; keys arm after
// the HUD fade-in) or a pulse-mode escalation (explicit rung, configured
// gate). Called after the checkin/escalation event is appended, so today's
// count includes the one being shown.
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
	if d.directCheckins() {
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

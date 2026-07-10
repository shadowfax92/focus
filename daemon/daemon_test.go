package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/ipc"
	"github.com/shadowfax92/focus/store"
)

func testDaemon(t *testing.T, now *time.Time, style string) *Daemon {
	t.Helper()
	cfg := config.Default()
	cfg.ReminderStyle = style
	cfg.Interval = time.Minute
	return &Daemon{
		cfg:       cfg,
		events:    store.New(filepath.Join(t.TempDir(), "events.jsonl")),
		statePath: filepath.Join(t.TempDir(), "state", "current.json"),
		state:     State{Position: cfg.Position},
		machine:   NewMachine(MachineState{}),
		now:       func() time.Time { return *now },
		idle:      func() float64 { return 0 },
		nextTick:  now.Add(cfg.Interval),
	}
}

func eventTypes(t *testing.T, d *Daemon) []string {
	t.Helper()
	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, len(events))
	for i, event := range events {
		types[i] = event.Type
	}
	return types
}

func wantEventTypes(t *testing.T, d *Daemon, want []string) {
	t.Helper()
	got := eventTypes(t, d)
	if len(got) != len(want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestHandleSetAckPauseResumeDoneRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StylePulse)

	requests := []ipc.Request{
		{Action: "set", Text: "ship backend"},
		{Action: "ack", Kind: "drifted"},
		{Action: "pause", Duration: "5m"},
		{Action: "resume"},
		{Action: "done"},
	}
	for _, request := range requests {
		response := d.Handle(request)
		if !response.OK {
			t.Fatalf("%s failed: %s", request.Action, response.Error)
		}
		now = now.Add(time.Second)
	}

	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"set", "pulse", "ack", "pause", "resume", "done"}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d: %+v", len(events), len(wantTypes), events)
	}
	for i, want := range wantTypes {
		if events[i].Type != want {
			t.Errorf("event %d type = %q, want %q", i, events[i].Type, want)
		}
	}
	if events[2].Kind != "drifted" {
		t.Fatalf("ack kind = %q, want drifted", events[2].Kind)
	}
	if d.state.FocusText != "" || d.machine.State().AwaitingAck {
		t.Fatalf("done did not clear state: %+v %+v", d.state, d.machine.State())
	}
}

func TestIdleGuardSkipsTicksAndPulsesOnReturn(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StylePulse)
	d.state.FocusText = "stay focused"
	d.state.SetAt = now.Add(-time.Hour)
	d.nextTick = now
	idle := 10 * time.Minute
	d.idle = func() float64 { return idle.Seconds() }

	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	if !d.idleGuarded || d.machine.State().AwaitingAck {
		t.Fatalf("idle tick was not skipped: guarded=%v state=%+v", d.idleGuarded, d.machine.State())
	}

	idle = 0
	now = now.Add(time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "idle_return" || events[1].Type != "pulse" {
		t.Fatalf("return events = %+v, want idle_return then pulse", events)
	}
	if state := d.machine.State(); state.Rung != 0 || !state.AwaitingAck {
		t.Fatalf("welcome-back pulse state = %+v", state)
	}
}

func TestResumePreservesTakeoverState(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StylePulse)
	d.state.FocusText = "finish the task"
	d.state.SetAt = now.Add(-time.Hour)
	d.machine.Start(now.Add(-2 * time.Minute))
	d.machine.Tick(now.Add(-time.Minute), 1)
	until := now.Add(time.Minute)
	d.state.PausedUntil = &until

	if err := d.resume(); err != nil {
		t.Fatal(err)
	}
	if d.state.PausedUntil != nil || !d.machine.State().InTakeover {
		t.Fatalf("resume lost takeover state: %+v", d.state)
	}
	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "resume" {
		t.Fatalf("resume events = %+v", events)
	}
}

func TestFullscreenTicksShowCheckinsNotEscalations(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)

	if response := d.Handle(ipc.Request{Action: "set", Text: "ship it"}); !response.OK {
		t.Fatal(response.Error)
	}
	// set must not fire an instant screen; the first check-in is a full interval out.
	wantEventTypes(t, d, []string{"set"})
	if d.machine.State().InTakeover {
		t.Fatal("set opened a takeover immediately")
	}

	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin"})
	if !d.machine.State().InTakeover {
		t.Fatal("check-in did not mark InTakeover")
	}

	// Unacked screens absorb further ticks — never a second checkin or an escalation.
	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin"})

	if response := d.Handle(ipc.Request{Action: "ack", Kind: "drifted"}); !response.OK {
		t.Fatal(response.Error)
	}
	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin", "ack", "checkin"})

	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	stats := store.DeriveToday(events, now, time.Local)
	if stats.Today.Distractions != 1 || stats.Today.Checkins != 2 {
		t.Fatalf("stats = %+v, want 1 distraction (drifted only) and 2 checkins", stats.Today)
	}
}

func TestFullscreenDoneAckSetsNextFocus(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)

	if response := d.Handle(ipc.Request{Action: "set", Text: "ship it"}); !response.OK {
		t.Fatal(response.Error)
	}
	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if response := d.Handle(ipc.Request{Action: "ack", Kind: "done", Text: "write the follow-up"}); !response.OK {
		t.Fatal(response.Error)
	}

	wantEventTypes(t, d, []string{"set", "checkin", "ack", "done", "set"})
	events, err := d.events.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	ack := events[2]
	if ack.Kind != "done" || ack.LatencyS == nil || *ack.LatencyS != 30 {
		t.Fatalf("done ack = %+v, want kind done with 30s latency", ack)
	}
	if events[4].Text != "write the follow-up" || d.state.FocusText != "write the follow-up" {
		t.Fatalf("next focus not set: event=%+v state=%q", events[4], d.state.FocusText)
	}
	if d.machine.State().InTakeover {
		t.Fatal("takeover state not cleared by done ack")
	}
}

func TestFullscreenDoneAckWithoutTextClearsFocus(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)

	if response := d.Handle(ipc.Request{Action: "set", Text: "ship it"}); !response.OK {
		t.Fatal(response.Error)
	}
	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	if response := d.Handle(ipc.Request{Action: "ack", Kind: "done"}); !response.OK {
		t.Fatal(response.Error)
	}

	wantEventTypes(t, d, []string{"set", "checkin", "ack", "done"})
	if d.state.FocusText != "" {
		t.Fatalf("focus not cleared: %q", d.state.FocusText)
	}
	// No focus → no further reminders until the next set.
	now = now.Add(d.cfg.Interval + time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin", "ack", "done"})
}

func TestFullscreenIdleReturnFiresWelcomeBackCheckin(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)
	d.state.FocusText = "stay focused"
	d.state.SetAt = now.Add(-time.Hour)
	d.nextTick = now
	idle := 10 * time.Minute
	d.idle = func() float64 { return idle.Seconds() }

	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	if !d.idleGuarded || d.machine.State().InTakeover {
		t.Fatalf("idle tick was not skipped: guarded=%v state=%+v", d.idleGuarded, d.machine.State())
	}

	idle = 0
	now = now.Add(time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"idle_return", "checkin"})
	if !d.machine.State().InTakeover {
		t.Fatal("welcome-back check-in not showing")
	}
}

func TestIdleReturnLeavesPendingCheckinAlone(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)
	d.state.FocusText = "stay focused"
	d.state.SetAt = now.Add(-time.Hour)
	d.machine.Checkin(now.Add(-time.Minute))
	reminderAt := d.machine.State().ReminderAt

	idle := 10 * time.Minute
	d.idle = func() float64 { return idle.Seconds() }
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	idle = 0
	now = now.Add(time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}

	// The screen from before the idle stretch is still up: no duplicate
	// checkin event, no restamped latency.
	wantEventTypes(t, d, []string{"idle_return"})
	state := d.machine.State()
	if !state.InTakeover || state.ReminderAt != reminderAt {
		t.Fatalf("pending check-in was disturbed: %+v", state)
	}
}

func TestSetClearsStaleIdleGuard(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)
	d.idleGuarded = true

	if response := d.Handle(ipc.Request{Action: "set", Text: "fresh start"}); !response.OK {
		t.Fatal(response.Error)
	}
	now = now.Add(time.Second)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set"})
	if d.machine.State().InTakeover {
		t.Fatal("stale idle guard grabbed the screen right after set")
	}
}

func TestPollClearsIdleGuardWithoutFocus(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)
	d.idleGuarded = true

	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	if d.idleGuarded {
		t.Fatal("idle guard survived a focus-less poll")
	}
}

func TestDoneAckWhilePausedClearsPause(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now, config.StyleFullscreen)

	if response := d.Handle(ipc.Request{Action: "set", Text: "ship it"}); !response.OK {
		t.Fatal(response.Error)
	}
	if response := d.Handle(ipc.Request{Action: "pause", Duration: "30m"}); !response.OK {
		t.Fatal(response.Error)
	}
	if response := d.Handle(ipc.Request{Action: "ack", Kind: "done"}); !response.OK {
		t.Fatal(response.Error)
	}
	if d.state.PausedUntil != nil {
		t.Fatalf("done ack left a dangling pause: %v", d.state.PausedUntil)
	}
}

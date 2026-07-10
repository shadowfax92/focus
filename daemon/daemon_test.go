package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/ipc"
	"github.com/shadowfax92/focus/store"
)

func testDaemon(t *testing.T, now *time.Time) *Daemon {
	t.Helper()
	cfg := config.Default()
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

func TestHandleSetAckPauseResumeDoneRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	d := testDaemon(t, &now)

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
	d := testDaemon(t, &now)
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
	d := testDaemon(t, &now)
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

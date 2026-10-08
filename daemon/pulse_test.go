package daemon

import (
	"testing"
	"time"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/ipc"
)

func fullscreenPulseDaemon(t *testing.T, now *time.Time) *Daemon {
	t.Helper()
	d := testDaemon(t, now, config.StyleFullscreen)
	d.cfg.Interval = 15 * time.Minute
	recordPresentedFocus(d)
	if response := d.Handle(ipc.Request{Action: "set", Text: "ship reminders"}); !response.OK {
		t.Fatal(response.Error)
	}
	return d
}

func TestFullscreenPulsesBetweenCheckins(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	now := start
	d := fullscreenPulseDaemon(t, &now)
	wantEventTypes(t, d, []string{"set"})

	for i, minute := range []int{5, 10, 15, 20, 25, 30} {
		now = start.Add(time.Duration(minute) * time.Minute)
		if err := d.poll(); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			wantEventTypes(t, d, []string{"set", "pulse"})
		case 1:
			wantEventTypes(t, d, []string{"set", "pulse", "pulse"})
		default:
			// The coincident 15m pulse loses to the check-in. Leaving the
			// screen unanswered also suppresses every subsequent glow.
			wantEventTypes(t, d, []string{"set", "pulse", "pulse", "checkin"})
		}
		state := d.machine.State()
		if state.Rung != 0 || state.UnackedPulses != 0 {
			t.Fatalf("passive glow advanced the ladder at %dm: %+v", minute, state)
		}
		if minute < 15 && state.AwaitingAck {
			t.Fatalf("passive glow requires acknowledgement at %dm: %+v", minute, state)
		}
	}
	if !d.machine.State().InTakeover {
		t.Fatal("15m check-in was not shown")
	}
}

func TestFullscreenPulseAckDoesNotDelayCheckin(t *testing.T) {
	for _, kind := range []string{"on_task", "drifted"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			now = start.Add(5 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			now = now.Add(3 * time.Second)
			if response := d.Handle(ipc.Request{Action: "ack", Kind: kind}); !response.OK {
				t.Fatalf("active glow ack: %s", response.Error)
			}
			if response := d.Handle(ipc.Request{Action: "ack"}); response.OK {
				t.Fatal("acknowledged the same passive glow twice")
			}
			now = start.Add(10 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			now = start.Add(15 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse", "ack", "pulse", "checkin"})
			events, err := d.events.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			if ack := events[2]; ack.Rung == nil || *ack.Rung != 0 || ack.LatencyS == nil || *ack.LatencyS != 3 {
				t.Fatalf("passive glow ack = %+v, want rung 0 and 3s latency", ack)
			}
		})
	}
}

func TestFullscreenPulseAckExpiresWithGlow(t *testing.T) {
	for _, seconds := range []int{3, 0} {
		t.Run(time.Duration(seconds).String(), func(t *testing.T) {
			now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			d := fullscreenPulseDaemon(t, &now)
			d.cfg.PulseSeconds = seconds
			now = now.Add(5 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			// The HUD uses its existing 8s fallback when pulse_seconds is 0.
			if seconds == 0 {
				seconds = 8
			}
			now = now.Add(time.Duration(seconds) * time.Second)
			if response := d.Handle(ipc.Request{Action: "ack"}); response.OK {
				t.Fatal("accepted an ack after the passive glow ended")
			}
			wantEventTypes(t, d, []string{"set", "pulse"})
		})
	}
}

func TestFullscreenPulseGuards(t *testing.T) {
	for _, guard := range []string{"idle", "paused", "no focus"} {
		t.Run(guard, func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			want := []string{"set"}
			switch guard {
			case "idle":
				d.idle = func() float64 { return (5 * time.Minute).Seconds() }
			case "paused":
				if response := d.Handle(ipc.Request{Action: "pause", Duration: "45m"}); !response.OK {
					t.Fatal(response.Error)
				}
				want = append(want, "pause")
			case "no focus":
				if response := d.Handle(ipc.Request{Action: "done"}); !response.OK {
					t.Fatal(response.Error)
				}
				want = append(want, "done")
			}
			for _, minute := range []int{5, 10, 15} {
				now = start.Add(time.Duration(minute) * time.Minute)
				if err := d.poll(); err != nil {
					t.Fatal(err)
				}
				wantEventTypes(t, d, want)
			}
		})
	}
}

func TestFullscreenPulseIntervalCanDisableNudges(t *testing.T) {
	for _, interval := range []time.Duration{0, 15 * time.Minute, 30 * time.Minute} {
		t.Run(interval.String(), func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			now := start
			d := testDaemon(t, &now, config.StyleFullscreen)
			d.cfg.Interval = 15 * time.Minute
			d.cfg.PulseInterval = interval
			if response := d.Handle(ipc.Request{Action: "set", Text: "quiet focus"}); !response.OK {
				t.Fatal(response.Error)
			}
			for _, minute := range []int{5, 10, 15} {
				now = start.Add(time.Duration(minute) * time.Minute)
				if err := d.poll(); err != nil {
					t.Fatal(err)
				}
				if minute < 15 {
					wantEventTypes(t, d, []string{"set"})
				} else {
					wantEventTypes(t, d, []string{"set", "checkin"})
				}
			}
		})
	}
}

func TestFullscreenLatePollPrioritizesCheckin(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	d := fullscreenPulseDaemon(t, &now)
	// Miss both glow deadlines, then wake with a check-in also overdue.
	now = now.Add(16 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "checkin"})
}

func TestFullscreenPulseSchedulesRestartAfterResume(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		name := "explicit resume"
		if automatic {
			name = "pause expires"
		}
		t.Run(name, func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			now := start
			d := fullscreenPulseDaemon(t, &now)
			if response := d.Handle(ipc.Request{Action: "pause", Duration: "3m"}); !response.OK {
				t.Fatal(response.Error)
			}
			now = start.Add(3 * time.Minute)
			if !automatic {
				if response := d.Handle(ipc.Request{Action: "resume"}); !response.OK {
					t.Fatal(response.Error)
				}
			}
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			now = start.Add(5 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pause", "resume"})
			now = start.Add(8 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pause", "resume", "pulse"})
			now = start.Add(18 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pause", "resume", "pulse", "checkin"})
		})
	}
}

func TestFullscreenPulseCannotBeAckedWhilePaused(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	d := fullscreenPulseDaemon(t, &now)
	now = now.Add(5 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	if response := d.Handle(ipc.Request{Action: "pause", Duration: "30m"}); !response.OK {
		t.Fatal(response.Error)
	}
	if response := d.Handle(ipc.Request{Action: "ack"}); response.OK {
		t.Fatal("accepted a passive glow ack while paused")
	}
	wantEventTypes(t, d, []string{"set", "pulse", "pause"})
}

func TestPulseStyleIgnoresPassivePulseInterval(t *testing.T) {
	for _, interval := range []time.Duration{0, 5 * time.Minute} {
		t.Run(interval.String(), func(t *testing.T) {
			start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
			now := start
			d := testDaemon(t, &now, config.StylePulse)
			d.cfg.Interval = 15 * time.Minute
			d.cfg.PulseInterval = interval
			if response := d.Handle(ipc.Request{Action: "set", Text: "existing ladder"}); !response.OK {
				t.Fatal(response.Error)
			}
			wantEventTypes(t, d, []string{"set", "pulse"})
			for _, minute := range []int{5, 10} {
				now = start.Add(time.Duration(minute) * time.Minute)
				if err := d.poll(); err != nil {
					t.Fatal(err)
				}
				wantEventTypes(t, d, []string{"set", "pulse"})
			}
			now = start.Add(15 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse", "pulse"})
			if state := d.machine.State(); state.Rung != 1 || state.UnackedPulses != 2 {
				t.Fatalf("existing second ladder rung changed: %+v", state)
			}
			now = start.Add(30 * time.Minute)
			if err := d.poll(); err != nil {
				t.Fatal(err)
			}
			wantEventTypes(t, d, []string{"set", "pulse", "pulse", "escalation"})
			if state := d.machine.State(); state.Rung != 2 || !state.InTakeover {
				t.Fatalf("existing pulse escalation changed: %+v", state)
			}
		})
	}
}

func TestFullscreenPulsePreservesFocusBudget(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	now := start
	d := testDaemon(t, &now, config.StyleFullscreen)
	d.cfg.Interval = 15 * time.Minute
	if response := d.Handle(ipc.Request{Action: "set", Text: "budgeted focus", Budget: "45m"}); !response.OK {
		t.Fatal(response.Error)
	}
	now = start.Add(5 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	if response := d.Handle(ipc.Request{Action: "ack"}); !response.OK {
		t.Fatal(response.Error)
	}
	status := d.Handle(ipc.Request{Action: "status"}).Status
	if status == nil || status.Budget != 45*time.Minute || status.ElapsedSeconds != 303 || status.SetAt == nil || !status.SetAt.Equal(start) {
		t.Fatalf("passive glow changed budget/focus clock: %+v", status)
	}
	saved, err := LoadState(d.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Budget != 45*time.Minute || saved.Machine.AwaitingAck {
		t.Fatalf("saved budget/passive acknowledgement state = %+v", saved)
	}
	now = start.Add(15 * time.Minute)
	if err := d.poll(); err != nil {
		t.Fatal(err)
	}
	wantEventTypes(t, d, []string{"set", "pulse", "ack", "checkin"})
}

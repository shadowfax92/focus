package daemon

import (
	"testing"
	"time"
)

func TestEscalationMachine(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		escalateAfter int
		want          []Action
	}{
		{
			name:          "default escalates after two unacked pulses",
			escalateAfter: 2,
			want: []Action{
				{Kind: ActionPulse, Rung: 0},
				{Kind: ActionPulse, Rung: 1},
				{Kind: ActionTakeover, Rung: 2},
				{},
			},
		},
		{
			name:          "one pulse then takeover",
			escalateAfter: 1,
			want: []Action{
				{Kind: ActionPulse, Rung: 0},
				{Kind: ActionTakeover, Rung: 1},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			machine := NewMachine(MachineState{})
			for i, want := range tc.want {
				var got Action
				if i == 0 {
					got = machine.Start(now)
				} else {
					got = machine.Tick(now.Add(time.Duration(i)*time.Minute), tc.escalateAfter)
				}
				if got != want {
					t.Fatalf("action %d = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

func TestAckResetsRung(t *testing.T) {
	machine := NewMachine(MachineState{})
	now := time.Now()
	machine.Start(now)
	machine.Tick(now.Add(time.Minute), 2)
	previous := machine.Ack()
	if previous.Rung != 1 || machine.State().AwaitingAck {
		t.Fatalf("unexpected ack state: previous=%+v current=%+v", previous, machine.State())
	}
	got := machine.Tick(now.Add(2*time.Minute), 2)
	if got.Kind != ActionPulse || got.Rung != 0 {
		t.Fatalf("first reminder after ack = %+v, want rung 0 pulse", got)
	}
}

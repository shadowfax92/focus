package daemon

import "time"

type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionPulse
	ActionTakeover
)

type Action struct {
	Kind ActionKind
	Rung int
}

type MachineState struct {
	Rung          int       `json:"rung"`
	UnackedPulses int       `json:"unacked_pulses"`
	AwaitingAck   bool      `json:"awaiting_ack"`
	InTakeover    bool      `json:"in_takeover"`
	ReminderAt    time.Time `json:"reminder_at,omitempty,omitzero"`
}

type Machine struct {
	state MachineState
}

func NewMachine(state MachineState) *Machine { return &Machine{state: state} }

func (m *Machine) State() MachineState { return m.state }

func (m *Machine) Reset() { m.state = MachineState{} }

func (m *Machine) Start(now time.Time) Action {
	m.state = MachineState{
		Rung:          0,
		UnackedPulses: 1,
		AwaitingAck:   true,
		ReminderAt:    now,
	}
	return Action{Kind: ActionPulse, Rung: 0}
}

func (m *Machine) Tick(now time.Time, escalateAfter int) Action {
	if !m.state.AwaitingAck {
		return m.Start(now)
	}
	if m.state.InTakeover {
		return Action{}
	}
	if m.state.UnackedPulses >= escalateAfter {
		m.state.Rung++
		m.state.InTakeover = true
		m.state.ReminderAt = now
		return Action{Kind: ActionTakeover, Rung: m.state.Rung}
	}
	m.state.Rung++
	m.state.UnackedPulses++
	m.state.ReminderAt = now
	return Action{Kind: ActionPulse, Rung: m.state.Rung}
}

func (m *Machine) Ack() MachineState {
	previous := m.state
	m.Reset()
	return previous
}

// Package hud is the only bridge between daemon policy and on-screen pixels.
//
// The real implementation is cgo Objective-C (hud_darwin.go + hud_darwin.m);
// non-darwin or cgo-disabled builds fall back to the headless stubs in
// hud_stub.go that log "[hud stub]" lines.
package hud

import "time"

type AckKind int

const (
	AckOnTask AckKind = iota
	AckDrifted
	AckRefocus
	// AckDone completes the current focus. newText carries the next focus
	// typed on the takeover, or "" when nothing comes next.
	AckDone
)

func (k AckKind) String() string {
	switch k {
	case AckOnTask:
		return "on_task"
	case AckDrifted:
		return "drifted"
	case AckRefocus:
		return "refocus"
	case AckDone:
		return "done"
	}
	return "unknown"
}

type Position struct {
	Preset string // "top-center" | "top-right" | "top-left" | "custom"
	X, Y   float64
}

type Config struct {
	IdleOpacity   float64
	Position      Position
	BreathingGate time.Duration
	PulseSeconds  int
}

type TakeoverContent struct {
	FocusText  string
	Quote      string
	MirrorLine string
	// Rung is echoed back in the ack; routine check-ins pass 0, pulse-mode
	// escalations the rung they escalated at.
	Rung int
	// Gate is the breathing-gate delay before the ack keys arm; 0 arms
	// immediately (the routine check-in default).
	Gate time.Duration
}

// Events are invoked from the UI thread; handlers must not block.
type Events struct {
	// OnAck fires for any UI ack: pill click while interactive, or takeover
	// keys. newText is non-empty only for AckRefocus. latency is measured from
	// when the pulse/takeover appeared. rung is the escalation rung acked.
	OnAck func(kind AckKind, rung int, latency time.Duration, newText string)
	// OnMoved fires after a pill drag ends, with the new window origin.
	OnMoved func(x, y float64)
}

// Run starts the Cocoa run loop and never returns. It must be called on the
// main goroutine (main.go locks it to the OS thread) after daemon policy
// goroutines are started.
func Run(cfg Config, ev Events) {
	runImpl(cfg, ev)
}

// SetFocus shows the pill with the given text; since drives the elapsed label.
func SetFocus(text string, since time.Time) {
	setFocusImpl(text, since)
}

// ClearFocus hides the pill.
func ClearFocus() {
	clearFocusImpl()
}

// Pulse plays the attention animation for the given escalation rung (0-based).
func Pulse(rung int) {
	pulseImpl(rung)
}

// ShowTakeover presents the full-screen ack takeover.
func ShowTakeover(c TakeoverContent) {
	showTakeoverImpl(c)
}

// DismissTakeover removes the takeover without an ack (e.g. CLI ack arrived).
func DismissTakeover() {
	dismissTakeoverImpl()
}

// SetPaused hides the pill while paused; the daemon also stops ticking.
func SetPaused(paused bool) {
	setPausedImpl(paused)
}

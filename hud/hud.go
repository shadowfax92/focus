// Package hud is the only bridge between daemon policy and on-screen pixels.
// The exported API below is FROZEN for the parallel build: the backend lane
// compiles against it, the UI lane implements it. Signature changes need
// orchestrator sign-off (see DESIGN.md).
//
// This file currently ships headless no-op stubs that log "[hud stub]" lines
// so the daemon can be exercised end-to-end before the real UI lands.
package hud

import (
	"fmt"
	"os"
	"time"
)

type AckKind int

const (
	AckOnTask AckKind = iota
	AckDrifted
	AckRefocus
)

func (k AckKind) String() string {
	switch k {
	case AckOnTask:
		return "on_task"
	case AckDrifted:
		return "drifted"
	case AckRefocus:
		return "refocus"
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
	Gate       time.Duration
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
	stub("run cfg=%+v", cfg)
	select {}
}

// SetFocus shows the pill with the given text; since drives the elapsed label.
func SetFocus(text string, since time.Time) {
	stub("set_focus %q since=%s", text, since.Format(time.RFC3339))
}

// ClearFocus hides the pill.
func ClearFocus() {
	stub("clear_focus")
}

// Pulse plays the attention animation for the given escalation rung (0-based).
func Pulse(rung int) {
	stub("pulse rung=%d", rung)
}

// ShowTakeover presents the full-screen ack takeover.
func ShowTakeover(c TakeoverContent) {
	stub("takeover focus=%q quote=%q mirror=%q gate=%s", c.FocusText, c.Quote, c.MirrorLine, c.Gate)
}

// DismissTakeover removes the takeover without an ack (e.g. CLI ack arrived).
func DismissTakeover() {
	stub("dismiss_takeover")
}

// SetPaused hides the pill while paused; the daemon also stops ticking.
func SetPaused(paused bool) {
	stub("set_paused %v", paused)
}

func stub(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[hud stub] "+format+"\n", args...)
}

//go:build darwin && cgo

package hud

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <stdlib.h>
#include "hud_darwin.h"
*/
import "C"

import (
	"time"
	"unsafe"
)

// events is written once by runImpl before the Cocoa loop starts, then only
// read from the main thread by the exported callbacks below.
var events Events

func runImpl(cfg Config, ev Events) {
	events = ev
	preset := C.CString(cfg.Position.Preset)
	defer C.free(unsafe.Pointer(preset))
	C.hudInit(C.double(cfg.IdleOpacity), preset,
		C.double(cfg.Position.X), C.double(cfg.Position.Y),
		C.int(cfg.PulseSeconds))
	C.hudRunApp() // never returns
}

func setFocusImpl(text string, since time.Time, budget time.Duration) {
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	C.hudSetFocus(ct, C.double(since.Unix()), C.longlong(budget))
}

func clearFocusImpl() {
	C.hudClearFocus()
}

func pulseImpl(rung int, reminderID uint64) {
	C.hudPulse(C.int(rung), C.ulonglong(reminderID))
}

func showTakeoverImpl(c TakeoverContent) {
	cf := C.CString(c.FocusText)
	cq := C.CString(c.Quote)
	cm := C.CString(c.MirrorLine)
	defer C.free(unsafe.Pointer(cf))
	defer C.free(unsafe.Pointer(cq))
	defer C.free(unsafe.Pointer(cm))
	C.hudShowTakeover(cf, cq, cm, C.int(c.Rung), C.double(c.Gate.Seconds()))
}

func dismissTakeoverImpl() {
	C.hudDismissTakeover()
}

func setPausedImpl(paused bool) {
	p := C.int(0)
	if paused {
		p = 1
	}
	C.hudSetPaused(p)
}

// The existing Cocoa timer owns repaint cadence and wall-clock measurement.
// It asks Go for all three chip runs in one call so tests and pixels share
// the formatter. These are C allocations; the caller must free each after
// copying.
//
//export goHudFormatPillTime
func goHudFormatPillTime(elapsedSeconds C.double, budgetNanos C.longlong, elapsed, budget, overage **C.char) {
	t := FormatPillTime(time.Duration(float64(elapsedSeconds)*float64(time.Second)), time.Duration(budgetNanos))
	*elapsed = C.CString(t.Elapsed)
	*budget = C.CString(t.Budget)
	*overage = C.CString(t.Overage)
}

//export goHudAck
func goHudAck(kind C.int, rung C.int, latency C.double, newText *C.char) {
	if events.OnAck == nil {
		return
	}
	events.OnAck(AckKind(kind), int(rung),
		time.Duration(float64(latency)*float64(time.Second)), C.GoString(newText))
}

// The click echoes the animation's ID from the Cocoa thread, so an async
// daemon handler can reject it after a newer reminder takes ownership.
//
//export goHudPassivePulseAck
func goHudPassivePulseAck(kind C.int, reminderID C.ulonglong, latency C.double) {
	if events.OnPassivePulseAck == nil {
		return
	}
	events.OnPassivePulseAck(AckKind(kind), uint64(reminderID),
		time.Duration(float64(latency)*float64(time.Second)))
}

//export goHudMoved
func goHudMoved(x C.double, y C.double) {
	if events.OnMoved == nil {
		return
	}
	events.OnMoved(float64(x), float64(y))
}

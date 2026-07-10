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

func setFocusImpl(text string, since time.Time) {
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	C.hudSetFocus(ct, C.double(since.Unix()))
}

func clearFocusImpl() {
	C.hudClearFocus()
}

func pulseImpl(rung int) {
	C.hudPulse(C.int(rung))
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

//export goHudAck
func goHudAck(kind C.int, rung C.int, latency C.double, newText *C.char) {
	if events.OnAck == nil {
		return
	}
	events.OnAck(AckKind(kind), int(rung),
		time.Duration(float64(latency)*float64(time.Second)), C.GoString(newText))
}

//export goHudMoved
func goHudMoved(x C.double, y C.double) {
	if events.OnMoved == nil {
		return
	}
	events.OnMoved(float64(x), float64(y))
}

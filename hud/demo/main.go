// Command demo drives every hud state without the daemon: pill, each pulse
// rung, and the takeover, plus an -auto script that synthesizes clicks, drags,
// and keystrokes through the real event path so OnAck/OnMoved are verifiable
// headlessly (no Accessibility permission needed).
package main

/*
#include <stdlib.h>
extern void hudTestKey(unsigned short keyCode, const char *chars);
extern void hudTestPillClick(int optionHeld);
extern void hudTestPillDrag(double dx, double dy);
extern void hudTestSnapshot(const char *pillPath, const char *takeoverPath);
*/
import "C"

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/shadowfax92/focus/hud"
)

// Cocoa needs the process main thread.
func init() { runtime.LockOSThread() }

func main() {
	text := flag.String("text", "ship the onboarding PR", "focus text")
	since := flag.Duration("since", 47*time.Minute, "how long ago the focus started")
	pill := flag.Bool("pill", false, "show the pill")
	pulse := flag.Int("pulse", -1, "fire a pulse at this rung (implies -pill)")
	pulseDelay := flag.Duration("pulse-delay", 1500*time.Millisecond, "delay before the pulse")
	pulseSeconds := flag.Int("pulse-seconds", 8, "rung-0 pulse duration (Config.PulseSeconds)")
	takeover := flag.Bool("takeover", false, "show the takeover (escalation style: rung 2, breathing gate)")
	checkin := flag.Bool("checkin", false, "show the routine check-in (rung 0, no gate, done hint)")
	takeoverDelay := flag.Duration("takeover-delay", 3*time.Second, "delay before the takeover/check-in")
	quote := flag.String("quote", "The main thing is to keep the main thing the main thing.", "takeover quote")
	mirror := flag.String("mirror", "2nd escalation today · yesterday: 5 · 43m on task", "takeover mirror footer")
	gate := flag.Duration("gate", 3*time.Second, "takeover breathing gate (0 = off)")
	idleOpacity := flag.Float64("idle-opacity", 0.30, "pill idle opacity")
	pos := flag.String("pos", "top-center", "pill position preset: top-center|top-right|top-left|custom")
	posX := flag.Float64("x", 0, "pill x for -pos custom")
	posY := flag.Float64("y", 0, "pill y for -pos custom")
	pause := flag.Duration("pause-test", 0, "SetPaused(true) after this delay, resume 2s later (0 = off)")
	auto := flag.String("auto", "", "comma-separated synthetic input: click|optclick|drag|enter|d|n|f|esc|type:<text>")
	snap := flag.String("snap", "", "write <prefix>-pill.png / <prefix>-takeover.png at -snap-delay")
	snapDelay := flag.Duration("snap-delay", 4*time.Second, "delay before -snap renders")
	autoDelay := flag.Duration("auto-delay", 2*time.Second, "wait after the last show step before -auto runs")
	autoStep := flag.Duration("auto-step", 1200*time.Millisecond, "spacing between -auto steps")
	exitAfter := flag.Duration("exit-after", 0, "quit after this long (0 = run until ^C)")
	flag.Parse()

	// -mirror's default reads like an escalation; give -checkin a fitting
	// default while still honoring an explicit -mirror.
	checkinMirror := "3rd check-in today · 43m on task · yesterday: 2 distractions"
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "mirror" {
			checkinMirror = *mirror
		}
	})

	ev := hud.Events{
		OnAck: func(kind hud.AckKind, rung int, latency time.Duration, newText string) {
			fmt.Printf("[demo] OnAck kind=%s rung=%d latency=%.2fs newText=%q\n",
				kind, rung, latency.Seconds(), newText)
			// Mimic the daemon: refocus/done-with-text set the new focus,
			// done-with-nothing clears it.
			if (kind == hud.AckRefocus || kind == hud.AckDone) && newText != "" {
				hud.SetFocus(newText, time.Now())
			} else if kind == hud.AckDone {
				hud.ClearFocus()
			}
		},
		OnMoved: func(x, y float64) {
			fmt.Printf("[demo] OnMoved x=%.0f y=%.0f\n", x, y)
		},
	}

	go func() {
		time.Sleep(600 * time.Millisecond)
		if *pill || *pulse >= 0 {
			hud.SetFocus(*text, time.Now().Add(-*since))
		}
		if *pulse >= 0 {
			time.Sleep(*pulseDelay)
			hud.Pulse(*pulse)
		}
		if *takeover {
			time.Sleep(*takeoverDelay)
			hud.ShowTakeover(hud.TakeoverContent{
				FocusText:  *text,
				Quote:      *quote,
				MirrorLine: *mirror,
				Rung:       2,
				Gate:       *gate,
			})
		}
		if *checkin {
			time.Sleep(*takeoverDelay)
			hud.ShowTakeover(hud.TakeoverContent{
				FocusText:  *text,
				Quote:      *quote,
				MirrorLine: checkinMirror,
			})
		}
		if *pause > 0 {
			time.Sleep(*pause)
			fmt.Println("[demo] SetPaused(true)")
			hud.SetPaused(true)
			time.Sleep(2 * time.Second)
			fmt.Println("[demo] SetPaused(false)")
			hud.SetPaused(false)
		}
		if *auto != "" {
			time.Sleep(*autoDelay)
			for _, step := range strings.Split(*auto, ",") {
				runAutoStep(strings.TrimSpace(step))
				time.Sleep(*autoStep)
			}
		}
	}()

	if *snap != "" {
		prefix := *snap
		time.AfterFunc(*snapDelay, func() {
			cp := C.CString(prefix + "-pill.png")
			ct := C.CString(prefix + "-takeover.png")
			defer C.free(unsafe.Pointer(cp))
			defer C.free(unsafe.Pointer(ct))
			C.hudTestSnapshot(cp, ct)
			fmt.Printf("[demo] snapshot -> %s-{pill,takeover}.png\n", prefix)
		})
	}

	if *exitAfter > 0 {
		time.AfterFunc(*exitAfter, func() { os.Exit(0) })
	}

	hud.Run(hud.Config{
		IdleOpacity:   *idleOpacity,
		Position:      hud.Position{Preset: *pos, X: *posX, Y: *posY},
		BreathingGate: *gate,
		PulseSeconds:  *pulseSeconds,
	}, ev)
}

func sendKey(code uint16, chars string) {
	cs := C.CString(chars)
	defer C.free(unsafe.Pointer(cs))
	C.hudTestKey(C.ushort(code), cs)
}

func runAutoStep(step string) {
	fmt.Printf("[demo] auto: %s\n", step)
	switch {
	case step == "click":
		C.hudTestPillClick(0)
	case step == "optclick":
		C.hudTestPillClick(1)
	case step == "drag":
		C.hudTestPillDrag(60, -80)
	case step == "enter":
		sendKey(36, "\r")
	case step == "d":
		sendKey(2, "d")
	case step == "n":
		sendKey(45, "n")
	case step == "f":
		sendKey(3, "f")
	case step == "esc":
		sendKey(53, "\x1b")
	case strings.HasPrefix(step, "type:"):
		for _, r := range step[len("type:"):] {
			sendKey(0, string(r))
		}
	default:
		fmt.Printf("[demo] unknown auto step %q\n", step)
	}
}

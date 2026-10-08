//go:build !darwin || !cgo

package hud

import (
	"fmt"
	"os"
	"time"
)

func runImpl(cfg Config, ev Events) {
	stub("run cfg=%+v", cfg)
	select {}
}

func setFocusImpl(text string, since time.Time, budget time.Duration) {
	stub("set_focus %q since=%s budget=%s", text, since.Format(time.RFC3339), budget)
}

func clearFocusImpl() {
	stub("clear_focus")
}

func pulseImpl(rung int, reminderID uint64) {
	if reminderID != 0 {
		stub("pulse rung=%d reminder_id=%d", rung, reminderID)
		return
	}
	stub("pulse rung=%d", rung)
}

func showTakeoverImpl(c TakeoverContent) {
	stub("takeover focus=%q quote=%q mirror=%q rung=%d gate=%s", c.FocusText, c.Quote, c.MirrorLine, c.Rung, c.Gate)
}

func dismissTakeoverImpl() {
	stub("dismiss_takeover")
}

func setPausedImpl(paused bool) {
	stub("set_paused %v", paused)
}

func stub(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[hud stub] "+format+"\n", args...)
}

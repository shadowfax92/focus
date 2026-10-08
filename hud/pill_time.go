package hud

import (
	"fmt"
	"time"
)

// PillTime is the pill's time readout, split into the runs the Cocoa time
// chip styles separately: bright elapsed, dim budget, and a red overage
// sub-chip. An empty Elapsed hides the chip entirely.
type PillTime struct {
	Elapsed string // "9m"; empty during an unbudgeted focus's first minute
	Budget  string // "/ 45m" when a budget is set
	Overage string // "+12m" once elapsed exceeds the budget
}

// FormatPillTime owns the pill's text rules so tests and pixels share them.
// Elapsed keeps the existing whole-minute display; exact durations determine
// overage so crossing a budget is visible even before the next whole minute.
func FormatPillTime(elapsed, budget time.Duration) PillTime {
	elapsed = max(elapsed, 0)
	minutes := int64(elapsed / time.Minute)
	if budget <= 0 {
		if minutes == 0 {
			return PillTime{}
		}
		return PillTime{Elapsed: pillMinutes(minutes)}
	}

	budgetText := budget.String()
	if budget%time.Minute == 0 {
		budgetText = pillMinutes(int64(budget / time.Minute))
	}
	t := PillTime{Elapsed: pillMinutes(minutes), Budget: "/ " + budgetText}
	if elapsed <= budget {
		return t
	}

	extra := elapsed - budget
	switch {
	case extra >= time.Minute:
		t.Overage = "+" + pillMinutes(int64(extra/time.Minute))
	case extra >= time.Second:
		t.Overage = fmt.Sprintf("+%ds", extra/time.Second)
	default:
		t.Overage = "+<1s"
	}
	return t
}

func pillMinutes(minutes int64) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours, remainder := minutes/60, minutes%60
	if remainder == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, remainder)
}

package hud

import (
	"fmt"
	"time"
)

// FormatPillTime supplies separate dim and warm text runs to the Cocoa pill.
// Elapsed keeps the existing whole-minute display; exact durations determine
// overage so crossing a budget is visible even before the next whole minute.
func FormatPillTime(elapsed, budget time.Duration) (suffix, overage string) {
	elapsed = max(elapsed, 0)
	minutes := int64(elapsed / time.Minute)
	if budget <= 0 {
		if minutes == 0 {
			return "", ""
		}
		return "· " + pillMinutes(minutes), ""
	}

	budgetText := budget.String()
	if budget%time.Minute == 0 {
		budgetText = pillMinutes(int64(budget / time.Minute))
	}
	suffix = fmt.Sprintf("· %s / %s", pillMinutes(minutes), budgetText)
	if elapsed <= budget {
		return suffix, ""
	}

	extra := elapsed - budget
	switch {
	case extra >= time.Minute:
		overage = pillMinutes(int64(extra / time.Minute))
	case extra >= time.Second:
		overage = fmt.Sprintf("%ds", extra/time.Second)
	default:
		overage = "<1s"
	}
	return suffix, "· +" + overage
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

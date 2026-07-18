package store

import (
	"sort"
	"time"
)

type DayStats struct {
	Date           string       `json:"date"`
	Distractions   int          `json:"distractions"`
	Pulses         int          `json:"pulses"`
	Checkins       int          `json:"checkins"`
	Escalations    int          `json:"escalations"`
	Acks           int          `json:"acks"`
	AvgAckLatencyS float64      `json:"avg_ack_latency_s"`
	Focuses        []FocusStats `json:"focuses,omitempty"`
	latencyTotal   float64
	latencyCount   int
}

type FocusStats struct {
	Focus          string  `json:"focus"`
	Distractions   int     `json:"distractions"`
	Pulses         int     `json:"pulses"`
	Checkins       int     `json:"checkins"`
	Escalations    int     `json:"escalations"`
	Acks           int     `json:"acks"`
	AvgAckLatencyS float64 `json:"avg_ack_latency_s"`
	latencyTotal   float64
	latencyCount   int
}

type TimelineEvent struct {
	TS       time.Time `json:"ts"`
	Type     string    `json:"type"`
	Kind     string    `json:"kind,omitempty"`
	Focus    string    `json:"focus,omitempty"`
	Rung     *int      `json:"rung,omitempty"`
	LatencyS *float64  `json:"latency_s,omitempty"`
}

type TodayStats struct {
	Today     DayStats `json:"today"`
	Yesterday DayStats `json:"yesterday"`
	DoDPct    *float64 `json:"dod_pct"`
}

type WeekStats struct {
	Start        string     `json:"start"`
	End          string     `json:"end"`
	Distractions int        `json:"distractions"`
	Daily        []DayStats `json:"daily"`
}

type WeeksStats struct {
	ThisWeek     WeekStats `json:"this_week"`
	LastWeek     WeekStats `json:"last_week"`
	WoWPct       *float64  `json:"wow_pct"`
	ImprovingRun int       `json:"improving_streak_days"`
}

// DeriveDays builds local-day aggregates and focus attribution from event history.
func DeriveDays(events []Event, end time.Time, days int, loc *time.Location) []DayStats {
	if days < 1 {
		return nil
	}
	end = dayStart(end.In(loc))
	start := end.AddDate(0, 0, -(days - 1))
	byDate := make(map[string]*DayStats, days)
	focusIndexes := make(map[string]map[string]int, days)
	result := make([]DayStats, days)
	for i := range days {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		result[i].Date = date
		byDate[date] = &result[i]
		focusIndexes[date] = make(map[string]int)
	}
	for _, attributed := range attributeEvents(events) {
		event := attributed.Event
		date := event.TS.In(loc).Format("2006-01-02")
		day := byDate[date]
		if day == nil {
			continue
		}
		var focus *FocusStats
		if contributesFocusContext(event.Type) {
			indexes := focusIndexes[date]
			index, ok := indexes[attributed.Focus]
			if !ok {
				index = len(day.Focuses)
				indexes[attributed.Focus] = index
				day.Focuses = append(day.Focuses, FocusStats{Focus: attributed.Focus})
			}
			focus = &day.Focuses[index]
		}
		switch event.Type {
		case "pulse":
			day.Pulses++
			focus.Pulses++
		// Routine fullscreen check-ins are reminders, not distractions —
		// only a drifted ack (or a pulse-mode escalation) moves the metric.
		case "checkin":
			day.Checkins++
			focus.Checkins++
		case "ack":
			day.Acks++
			focus.Acks++
			if event.Kind == "drifted" {
				day.Distractions++
				focus.Distractions++
			}
			if event.LatencyS != nil {
				day.latencyTotal += *event.LatencyS
				day.latencyCount++
				focus.latencyTotal += *event.LatencyS
				focus.latencyCount++
			}
		case "escalation":
			day.Escalations++
			day.Distractions++
			focus.Escalations++
			focus.Distractions++
		}
	}
	for i := range result {
		day := &result[i]
		if day.latencyCount > 0 {
			day.AvgAckLatencyS = day.latencyTotal / float64(day.latencyCount)
		}
		day.latencyCount = 0
		day.latencyTotal = 0
		for j := range day.Focuses {
			focus := &day.Focuses[j]
			if focus.latencyCount > 0 {
				focus.AvgAckLatencyS = focus.latencyTotal / float64(focus.latencyCount)
			}
			focus.latencyCount = 0
			focus.latencyTotal = 0
		}
		sort.SliceStable(day.Focuses, func(a, b int) bool {
			return day.Focuses[a].Distractions > day.Focuses[b].Distractions
		})
	}
	return result
}

// DeriveTimeline returns attributed events in stable chronological order and local time.
func DeriveTimeline(events []Event, start, end time.Time, loc *time.Location) []TimelineEvent {
	result := make([]TimelineEvent, 0)
	for _, attributed := range attributeEvents(events) {
		if attributed.Event.TS.Before(start) || !attributed.Event.TS.Before(end) {
			continue
		}
		result = append(result, TimelineEvent{
			TS:       attributed.Event.TS.In(loc),
			Type:     attributed.Event.Type,
			Kind:     attributed.Event.Kind,
			Focus:    attributed.Focus,
			Rung:     attributed.Event.Rung,
			LatencyS: attributed.Event.LatencyS,
		})
	}
	return result
}

type attributedEvent struct {
	Event Event
	Focus string
}

// attributeEvents reconstructs focus context without mutating append-order history.
func attributeEvents(events []Event) []attributedEvent {
	ordered := append([]Event(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].TS.Before(ordered[j].TS)
	})
	result := make([]attributedEvent, 0, len(ordered))
	activeFocus := ""
	for _, event := range ordered {
		if event.Type == "set" {
			activeFocus = event.Text
		}
		result = append(result, attributedEvent{Event: event, Focus: activeFocus})
		if event.Type == "done" {
			activeFocus = ""
		}
	}
	return result
}

func contributesFocusContext(eventType string) bool {
	switch eventType {
	case "set", "checkin", "pulse", "ack", "escalation", "done", "pause", "resume", "idle_return":
		return true
	default:
		return false
	}
}

func DeriveToday(events []Event, now time.Time, loc *time.Location) TodayStats {
	days := DeriveDays(events, now, 2, loc)
	result := TodayStats{Yesterday: days[0], Today: days[1]}
	result.DoDPct = percent(result.Today.Distractions, result.Yesterday.Distractions)
	return result
}

func DeriveWeeks(events []Event, now time.Time, loc *time.Location) WeeksStats {
	today := dayStart(now.In(loc))
	thisStart := today.AddDate(0, 0, -weekdayIndex(today.Weekday()))
	lastStart := thisStart.AddDate(0, 0, -7)
	days := DeriveDays(events, today, weekdayIndex(today.Weekday())+8, loc)

	thisDays := make([]DayStats, 0, 7)
	lastDays := make([]DayStats, 0, 7)
	for _, day := range days {
		t, _ := time.ParseInLocation("2006-01-02", day.Date, loc)
		if t.Before(thisStart) {
			lastDays = append(lastDays, day)
		} else {
			thisDays = append(thisDays, day)
		}
	}
	result := WeeksStats{
		ThisWeek: weekFromDays(thisStart, thisDays),
		LastWeek: weekFromDays(lastStart, lastDays),
	}
	result.WoWPct = percent(result.ThisWeek.Distractions, result.LastWeek.Distractions)
	result.ImprovingRun = improvingStreak(events, today, loc)
	return result
}

func percent(current, previous int) *float64 {
	if previous == 0 {
		return nil
	}
	v := float64(current-previous) / float64(previous) * 100
	return &v
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func weekdayIndex(day time.Weekday) int {
	if day == time.Sunday {
		return 6
	}
	return int(day - time.Monday)
}

func weekFromDays(start time.Time, days []DayStats) WeekStats {
	w := WeekStats{
		Start: start.Format("2006-01-02"),
		End:   start.AddDate(0, 0, 6).Format("2006-01-02"),
		Daily: days,
	}
	for _, day := range days {
		w.Distractions += day.Distractions
	}
	return w
}

func improvingStreak(events []Event, today time.Time, loc *time.Location) int {
	if len(events) == 0 {
		return 0
	}
	oldest := today
	for _, event := range events {
		d := dayStart(event.TS.In(loc))
		if d.Before(oldest) {
			oldest = d
		}
	}
	n := int(today.Sub(oldest).Hours()/24) + 1
	days := DeriveDays(events, today, n, loc)
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	streak := 0
	for i := len(days) - 1; i > 0; i-- {
		if days[i].Distractions < days[i-1].Distractions {
			streak++
			continue
		}
		break
	}
	return streak
}

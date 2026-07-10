package store

import (
	"sort"
	"time"
)

type DayStats struct {
	Date           string  `json:"date"`
	Distractions   int     `json:"distractions"`
	Pulses         int     `json:"pulses"`
	Checkins       int     `json:"checkins"`
	Acks           int     `json:"acks"`
	AvgAckLatencyS float64 `json:"avg_ack_latency_s"`
	latencyTotal   float64
	latencyCount   int
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

func DeriveDays(events []Event, end time.Time, days int, loc *time.Location) []DayStats {
	if days < 1 {
		return nil
	}
	end = dayStart(end.In(loc))
	start := end.AddDate(0, 0, -(days - 1))
	byDate := make(map[string]*DayStats, days)
	result := make([]DayStats, days)
	for i := range days {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		result[i].Date = date
		byDate[date] = &result[i]
	}
	for _, event := range events {
		date := event.TS.In(loc).Format("2006-01-02")
		day := byDate[date]
		if day == nil {
			continue
		}
		switch event.Type {
		case "pulse":
			day.Pulses++
		// Routine fullscreen check-ins are reminders, not distractions —
		// only a drifted ack (or a pulse-mode escalation) moves the metric.
		case "checkin":
			day.Checkins++
		case "ack":
			day.Acks++
			if event.Kind == "drifted" {
				day.Distractions++
			}
			if event.LatencyS != nil {
				day.latencyTotal += *event.LatencyS
				day.latencyCount++
			}
		case "escalation":
			day.Distractions++
		}
	}
	for i := range result {
		if result[i].latencyCount > 0 {
			result[i].AvgAckLatencyS = result[i].latencyTotal / float64(result[i].latencyCount)
		}
		result[i].latencyCount = 0
		result[i].latencyTotal = 0
	}
	return result
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

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeriveDaysDistractionAndLatency(t *testing.T) {
	loc := time.FixedZone("test", -7*60*60)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, loc)
	latencyA, latencyB := 2.0, 6.0
	events := []Event{
		{TS: now.Add(-time.Hour), Type: "pulse", Rung: Rung(0)},
		{TS: now.Add(-50 * time.Minute), Type: "ack", Kind: "on_task", LatencyS: &latencyA},
		{TS: now.Add(-40 * time.Minute), Type: "ack", Kind: "drifted", LatencyS: &latencyB},
		{TS: now.Add(-30 * time.Minute), Type: "escalation"},
		{TS: now.AddDate(0, 0, -1), Type: "ack", Kind: "drifted"},
	}
	got := DeriveToday(events, now, loc)
	if got.Today.Distractions != 2 || got.Today.Pulses != 1 || got.Today.Acks != 2 || got.Today.Escalations != 1 {
		t.Fatalf("unexpected today stats: %+v", got.Today)
	}
	if got.Today.AvgAckLatencyS != 4 {
		t.Fatalf("avg latency = %v, want 4", got.Today.AvgAckLatencyS)
	}
	if got.Yesterday.Distractions != 1 || got.DoDPct == nil || *got.DoDPct != 100 {
		t.Fatalf("unexpected comparison: %+v", got)
	}
}

func TestCheckinsAreNotDistractions(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, loc)
	latency := 1.5
	events := []Event{
		{TS: now.Add(-3 * time.Hour), Type: "checkin"},
		{TS: now.Add(-3 * time.Hour), Type: "ack", Kind: "on_task", LatencyS: &latency},
		{TS: now.Add(-2 * time.Hour), Type: "checkin"},
		{TS: now.Add(-2 * time.Hour), Type: "ack", Kind: "drifted", LatencyS: &latency},
		{TS: now.Add(-time.Hour), Type: "checkin"},
		{TS: now.Add(-time.Hour), Type: "ack", Kind: "done", LatencyS: &latency},
		{TS: now.Add(-time.Hour), Type: "done"},
		{TS: now.Add(-time.Hour), Type: "set", Text: "next thing"},
	}
	got := DeriveToday(events, now, loc)
	if got.Today.Checkins != 3 {
		t.Fatalf("checkins = %d, want 3", got.Today.Checkins)
	}
	if got.Today.Distractions != 1 {
		t.Fatalf("distractions = %d, want 1 (drifted ack only)", got.Today.Distractions)
	}
	if got.Today.Acks != 3 || got.Today.AvgAckLatencyS != 1.5 {
		t.Fatalf("acks/latency = %d/%v, want 3/1.5", got.Today.Acks, got.Today.AvgAckLatencyS)
	}
}

func TestDeriveTodayAttributesFocusLifecycle(t *testing.T) {
	loc := time.FixedZone("test", -7*60*60)
	now := time.Date(2026, 7, 10, 15, 0, 0, 0, loc)
	latency := 4.5
	events := []Event{
		{TS: now.AddDate(0, 0, -1).Add(8*time.Hour + 50*time.Minute), Type: "set", Text: "Build stats"},
		{TS: now.Add(-6 * time.Hour), Type: "checkin"},
		{TS: now.Add(-6 * time.Hour), Type: "ack", Kind: "drifted", LatencyS: &latency},
		{TS: now.Add(-5 * time.Hour), Type: "set", Text: "Email"},
		{TS: now.Add(-4 * time.Hour), Type: "escalation", Rung: Rung(2)},
		{TS: now.Add(-3 * time.Hour), Type: "done"},
		{TS: now.Add(-2 * time.Hour), Type: "escalation", Rung: Rung(3)},
		{TS: now.Add(-90 * time.Minute), Type: "set", Text: "Build stats"},
		{TS: now.Add(-time.Hour), Type: "ack", Kind: "drifted", LatencyS: &latency},
		{TS: now.Add(-30 * time.Minute), Type: "checkin"},
	}

	got := DeriveToday(events, now, loc)
	if got.Today.Distractions != 4 {
		t.Fatalf("distractions = %d, want 4", got.Today.Distractions)
	}
	if len(got.Today.Focuses) != 3 {
		t.Fatalf("focuses = %+v, want 3 summaries", got.Today.Focuses)
	}
	assertFocusStats(t, got.Today.Focuses[0], FocusStats{
		Focus: "Build stats", Distractions: 2, Checkins: 2, Acks: 2, AvgAckLatencyS: 4.5,
	})
	assertFocusStats(t, got.Today.Focuses[1], FocusStats{
		Focus: "Email", Distractions: 1, Escalations: 1,
	})
	assertFocusStats(t, got.Today.Focuses[2], FocusStats{
		Distractions: 1, Escalations: 1,
	})
}

func TestDeriveTodayCarriesFocusAcrossLocalMidnight(t *testing.T) {
	loc := time.FixedZone("test", 5*60*60+30*60)
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, loc)
	events := []Event{
		{TS: time.Date(2026, 7, 9, 23, 55, 0, 0, loc), Type: "set", Text: "Overnight task"},
		{TS: time.Date(2026, 7, 10, 0, 5, 0, 0, loc), Type: "ack", Kind: "drifted"},
	}

	got := DeriveToday(events, now, loc)
	if len(got.Today.Focuses) != 1 || got.Today.Focuses[0].Focus != "Overnight task" || got.Today.Focuses[0].Distractions != 1 {
		t.Fatalf("today focuses = %+v, want overnight task with one distraction", got.Today.Focuses)
	}
	if len(got.Yesterday.Focuses) != 1 || got.Yesterday.Focuses[0].Focus != "Overnight task" {
		t.Fatalf("yesterday focuses = %+v, want set focus", got.Yesterday.Focuses)
	}
}

func TestDeriveTimelineIsStableChronologicalAndLocal(t *testing.T) {
	loc := time.FixedZone("test", -7*60*60)
	start := time.Date(2026, 7, 10, 0, 0, 0, 0, loc)
	t1 := start.Add(9 * time.Hour)
	t2 := start.Add(10 * time.Hour)
	t3 := start.Add(11 * time.Hour)
	events := []Event{
		{TS: t3, Type: "checkin"},
		{TS: t1, Type: "set", Text: "First"},
		{TS: t2, Type: "ack", Kind: "done", LatencyS: Latency(3.2)},
		{TS: t2, Type: "done"},
		{TS: t2, Type: "set", Text: "Next"},
	}

	got := DeriveTimeline(events, start, start.AddDate(0, 0, 1), loc)
	if len(got) != 5 {
		t.Fatalf("timeline = %+v, want 5 events", got)
	}
	wantTypes := []string{"set", "ack", "done", "set", "checkin"}
	wantFocuses := []string{"First", "First", "First", "Next", "Next"}
	for i := range got {
		if got[i].Type != wantTypes[i] || got[i].Focus != wantFocuses[i] {
			t.Errorf("timeline[%d] = %+v, want type %q focus %q", i, got[i], wantTypes[i], wantFocuses[i])
		}
		if got[i].TS.Location() != loc {
			t.Errorf("timeline[%d] location = %v, want %v", i, got[i].TS.Location(), loc)
		}
	}
	if got[1].LatencyS == nil || *got[1].LatencyS != 3.2 {
		t.Fatalf("ack metadata = %+v, want latency 3.2", got[1])
	}
}

func assertFocusStats(t *testing.T, got, want FocusStats) {
	t.Helper()
	if got.Focus != want.Focus ||
		got.Distractions != want.Distractions ||
		got.Pulses != want.Pulses ||
		got.Checkins != want.Checkins ||
		got.Escalations != want.Escalations ||
		got.Acks != want.Acks ||
		got.AvgAckLatencyS != want.AvgAckLatencyS {
		t.Fatalf("focus stats = %+v, want %+v", got, want)
	}
}

func TestDeriveWeeksAndImprovingStreak(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, loc) // Friday
	var events []Event
	for day, count := range []int{5, 4, 3} {
		date := now.AddDate(0, 0, day-2)
		for range count {
			events = append(events, Event{TS: date, Type: "escalation"})
		}
	}
	weeks := DeriveWeeks(events, now, loc)
	if weeks.ThisWeek.Distractions != 12 {
		t.Fatalf("this week distractions = %d, want 12", weeks.ThisWeek.Distractions)
	}
	if weeks.ImprovingRun != 2 {
		t.Fatalf("improving streak = %d, want 2", weeks.ImprovingRun)
	}
}

func TestAppendNormalizesTimestampToUTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	events := New(path)
	when := time.Date(2026, 7, 10, 8, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	if err := events.Append(Event{TS: when, Type: "set", Text: "test"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ts":"2026-07-10T15:00:00Z"`) {
		t.Fatalf("timestamp was not normalized to UTC: %s", b)
	}
}

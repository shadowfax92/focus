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
	if got.Today.Distractions != 2 || got.Today.Pulses != 1 || got.Today.Acks != 2 {
		t.Fatalf("unexpected today stats: %+v", got.Today)
	}
	if got.Today.AvgAckLatencyS != 4 {
		t.Fatalf("avg latency = %v, want 4", got.Today.AvgAckLatencyS)
	}
	if got.Yesterday.Distractions != 1 || got.DoDPct == nil || *got.DoDPct != 100 {
		t.Fatalf("unexpected comparison: %+v", got)
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

package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shadowfax92/focus/store"
)

func TestStatsDetailedFlagAndRangeConflicts(t *testing.T) {
	flag := statsCmd.Flags().Lookup("detailed")
	if flag == nil || flag.Shorthand != "d" {
		t.Fatalf("detailed flag = %+v, want -d shorthand", flag)
	}
	for _, tc := range []struct {
		name string
		args []string
		days int
	}{
		{name: "days", days: 3},
		{name: "weeks", args: []string{"weeks"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateStatsOptions(tc.args, tc.days, true); err == nil || !strings.Contains(err.Error(), "today's stats") {
				t.Fatalf("validateStatsOptions() error = %v, want detailed today conflict", err)
			}
		})
	}
	if err := validateStatsOptions(nil, 0, true); err != nil {
		t.Fatalf("today detailed validation failed: %v", err)
	}
}

func TestWriteFocusSummary(t *testing.T) {
	var output strings.Builder
	writeFocusSummary(&output, []store.FocusStats{
		{Focus: "Build\n  stats", Distractions: 4},
		{Distractions: 1},
		{Focus: "Inbox", Distractions: 0},
	})
	want := "Distractions by focus\n  4  Build stats\n  1  (unattributed)\n  0  Inbox\n"
	if output.String() != want {
		t.Fatalf("focus summary:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestWriteTimeline(t *testing.T) {
	loc := time.FixedZone("test", -7*60*60)
	day := time.Date(2026, 7, 10, 0, 0, 0, 0, loc)
	timeline := []store.TimelineEvent{
		{TS: day.Add(9 * time.Hour), Type: "set", Focus: "Build stats"},
		{TS: day.Add(9*time.Hour + 15*time.Minute), Type: "checkin", Focus: "Build stats"},
		{TS: day.Add(9*time.Hour + 30*time.Minute), Type: "escalation", Focus: "Build stats", Rung: store.Rung(2)},
		{TS: day.Add(9*time.Hour + 31*time.Minute), Type: "ack", Kind: "drifted", Focus: "Build stats", LatencyS: store.Latency(4.2)},
	}
	var output strings.Builder
	writeTimeline(&output, day, timeline)
	want := "Timeline · Jul 10\n" +
		"  9:00 AM  Focus set   Build stats\n" +
		"  9:15 AM  Check-in    Build stats\n" +
		"  9:30 AM  Escalation  Build stats · rung 2\n" +
		"  9:31 AM  Distracted  Build stats · 4.2s\n"
	if output.String() != want {
		t.Fatalf("timeline:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestDetailedTodayJSONKeepsSummaryAndAddsTimeline(t *testing.T) {
	loc := time.FixedZone("test", -7*60*60)
	stats := store.TodayStats{
		Today: store.DayStats{
			Date:         "2026-07-10",
			Distractions: 1,
			Focuses:      []store.FocusStats{{Focus: "Build stats", Distractions: 1}},
		},
		Yesterday: store.DayStats{Date: "2026-07-09"},
	}
	timeline := []store.TimelineEvent{{
		TS: time.Date(2026, 7, 10, 9, 0, 0, 0, loc), Type: "set", Focus: "Build stats",
	}}
	var output strings.Builder
	if err := writeJSON(&output, detailedTodayStats{TodayStats: stats, Timeline: timeline}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(output.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got["today"] == nil || got["yesterday"] == nil {
		t.Fatalf("summary fields missing from detailed JSON: %s", output.String())
	}
	events, ok := got["timeline"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("timeline missing from detailed JSON: %s", output.String())
	}
	event := events[0].(map[string]any)
	if event["focus"] != "Build stats" || !strings.HasSuffix(event["ts"].(string), "-07:00") {
		t.Fatalf("timeline event = %+v, want structured focus and local timestamp", event)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("detailed JSON contains ANSI: %q", output.String())
	}
}

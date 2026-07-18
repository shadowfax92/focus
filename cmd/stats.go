package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/render"
	"github.com/shadowfax92/focus/store"
)

var (
	statsDays     int
	statsJSON     bool
	statsDetailed bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [weeks]",
	Short: "Show distraction stats",
	Long:  "Show distraction stats with per-focus attribution. Use --detailed for today's local-time event timeline.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateStatsOptions(args, statsDays, statsDetailed); err != nil {
			return err
		}
		events, err := store.Default().ReadAll()
		if err != nil {
			return err
		}
		now := time.Now()
		switch {
		case len(args) == 1:
			return printWeeks(store.DeriveWeeks(events, now, time.Local), statsJSON)
		case statsDays > 0:
			return printDays(store.DeriveDays(events, now, statsDays, time.Local), statsJSON)
		default:
			stats := store.DeriveToday(events, now, time.Local)
			if !statsDetailed {
				return printToday(stats, statsJSON)
			}
			localNow := now.In(time.Local)
			start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
			timeline := store.DeriveTimeline(events, start, start.AddDate(0, 0, 1), time.Local)
			return printDetailedToday(stats, timeline, start, statsJSON)
		}
	},
}

type detailedTodayStats struct {
	store.TodayStats
	Timeline []store.TimelineEvent `json:"timeline"`
}

// validateStatsOptions keeps detailed timelines scoped to the today view.
func validateStatsOptions(args []string, days int, detailed bool) error {
	if len(args) == 1 && args[0] != "weeks" {
		return fmt.Errorf("unknown stats view %q (expected weeks)", args[0])
	}
	if len(args) == 1 && days != 0 {
		return fmt.Errorf("--days cannot be combined with the weeks view")
	}
	if days < 0 {
		return fmt.Errorf("--days must be positive")
	}
	if detailed && (len(args) == 1 || days > 0) {
		return fmt.Errorf("--detailed is only available for today's stats")
	}
	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printJSON(value any) error {
	return writeJSON(os.Stdout, value)
}

// printToday renders the existing headline metrics and ranked focus attribution.
func printToday(stats store.TodayStats, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(stats)
	}
	render.CyanBold.Println("Focus today")
	fmt.Printf("  Distractions  ")
	render.Bold.Printf("%d", stats.Today.Distractions)
	fmt.Printf("  vs %d yesterday  ", stats.Yesterday.Distractions)
	render.PctColorInt(stats.Today.Distractions, stats.Yesterday.Distractions).
		Printf("%s\n", render.FormatPctInt(stats.Today.Distractions, stats.Yesterday.Distractions))
	fmt.Printf("  Check-ins    %d\n", stats.Today.Checkins)
	fmt.Printf("  Pulses       %d\n", stats.Today.Pulses)
	fmt.Printf("  Acks         %d\n", stats.Today.Acks)
	fmt.Printf("  Avg latency  %.1fs\n", stats.Today.AvgAckLatencyS)
	writeFocusSummary(os.Stdout, stats.Today.Focuses)
	return nil
}

// printDetailedToday adds a human or structured timeline to today's summary.
func printDetailedToday(stats store.TodayStats, timeline []store.TimelineEvent, day time.Time, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(detailedTodayStats{TodayStats: stats, Timeline: timeline})
	}
	if err := printToday(stats, false); err != nil {
		return err
	}
	fmt.Println()
	writeTimeline(os.Stdout, day, timeline)
	return nil
}

// writeFocusSummary renders task attribution in store-ranked order.
func writeFocusSummary(w io.Writer, focuses []store.FocusStats) {
	fmt.Fprintln(w, "Distractions by focus")
	if len(focuses) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, focus := range focuses {
		fmt.Fprintf(w, "  %d  %s\n", focus.Distractions, focusLabel(focus.Focus))
	}
}

// writeTimeline renders structured events as a local-time chronological list.
func writeTimeline(w io.Writer, day time.Time, timeline []store.TimelineEvent) {
	fmt.Fprintf(w, "Timeline · %s\n", day.Format("Jan 2"))
	if len(timeline) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	labels := make([]string, len(timeline))
	labelWidth := 0
	for i, event := range timeline {
		labels[i] = timelineLabel(event)
		labelWidth = max(labelWidth, len(labels[i]))
	}
	for i, event := range timeline {
		details := []string{focusLabel(event.Focus)}
		if event.Rung != nil {
			details = append(details, fmt.Sprintf("rung %d", *event.Rung))
		}
		if event.LatencyS != nil {
			details = append(details, fmt.Sprintf("%.1fs", *event.LatencyS))
		}
		fmt.Fprintf(w, "  %s  %-*s  %s\n",
			event.TS.Format("3:04 PM"), labelWidth, labels[i], strings.Join(details, " · "))
	}
}

func timelineLabel(event store.TimelineEvent) string {
	switch event.Type {
	case "set":
		return "Focus set"
	case "checkin":
		return "Check-in"
	case "pulse":
		return "Pulse"
	case "escalation":
		return "Escalation"
	case "ack":
		switch event.Kind {
		case "on_task":
			return "On task"
		case "drifted":
			return "Distracted"
		case "refocus":
			return "Refocused"
		case "done":
			return "Completed"
		default:
			return "Acknowledged"
		}
	case "done":
		return "Focus done"
	case "pause":
		return "Paused"
	case "resume":
		return "Resumed"
	case "idle_return":
		return "Returned"
	default:
		return event.Type
	}
}

func focusLabel(focus string) string {
	focus = strings.Join(strings.Fields(focus), " ")
	if focus == "" {
		return "(unattributed)"
	}
	return focus
}

func printDays(days []store.DayStats, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(days)
	}
	values := make([]int, len(days))
	labels := make([]string, len(days))
	for i, day := range days {
		values[i] = day.Distractions
		date, _ := time.Parse("2006-01-02", day.Date)
		labels[i] = date.Format("Jan 2")
	}
	render.CyanBold.Printf("Distractions · last %d days\n", len(days))
	render.VerticalBars(values, labels, render.CyanBold)
	return nil
}

func printWeeks(stats store.WeeksStats, jsonOutput bool) error {
	if jsonOutput {
		return printJSON(stats)
	}
	render.CyanBold.Println("Focus weeks")
	fmt.Printf("  This week     ")
	render.Bold.Printf("%d", stats.ThisWeek.Distractions)
	fmt.Printf("  vs %d last week  ", stats.LastWeek.Distractions)
	render.PctColorInt(stats.ThisWeek.Distractions, stats.LastWeek.Distractions).
		Printf("%s\n", render.FormatPctInt(stats.ThisWeek.Distractions, stats.LastWeek.Distractions))
	fmt.Printf("  This week     %s\n", render.Sparkline(dayValues(stats.ThisWeek.Daily)))
	fmt.Printf("  Last week     %s\n", render.Sparkline(dayValues(stats.LastWeek.Daily)))
	if stats.ImprovingRun > 0 {
		render.GreenBold.Printf("  %d days improving\n", stats.ImprovingRun)
	} else {
		render.Dim.Println("  No improving streak yet")
	}
	return nil
}

func dayValues(days []store.DayStats) []int {
	values := make([]int, len(days))
	for i, day := range days {
		values[i] = day.Distractions
	}
	return values
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 0, "show a daily chart for the last N days")
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "print JSON")
	statsCmd.Flags().BoolVarP(&statsDetailed, "detailed", "d", false, "show today's chronological event timeline")
	rootCmd.AddCommand(statsCmd)
}

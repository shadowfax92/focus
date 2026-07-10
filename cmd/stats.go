package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/render"
	"github.com/shadowfax92/focus/store"
)

var (
	statsDays int
	statsJSON bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [weeks]",
	Short: "Show distraction stats",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 && args[0] != "weeks" {
			return fmt.Errorf("unknown stats view %q (expected weeks)", args[0])
		}
		if len(args) == 1 && statsDays != 0 {
			return fmt.Errorf("--days cannot be combined with the weeks view")
		}
		if statsDays < 0 {
			return fmt.Errorf("--days must be positive")
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
			return printToday(store.DeriveToday(events, now, time.Local), statsJSON)
		}
	},
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

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
	fmt.Printf("  Pulses       %d\n", stats.Today.Pulses)
	fmt.Printf("  Acks         %d\n", stats.Today.Acks)
	fmt.Printf("  Avg latency  %.1fs\n", stats.Today.AvgAckLatencyS)
	return nil
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
	rootCmd.AddCommand(statsCmd)
}

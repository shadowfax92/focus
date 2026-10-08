package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/ipc"
)

var setCmd = &cobra.Command{
	Use:   "set <text> [budget]",
	Short: "Set or replace the current focus",
	Long:  "Set or replace the current focus, with an optional positive Go-style time budget (for example 45m or 1h30m).",
	Args: func(cmd *cobra.Command, args []string) error {
		if setHelpRequested(cmd, args) {
			return nil
		}
		return cobra.RangeArgs(1, 2)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if setHelpRequested(cmd, args) {
			return cmd.Help()
		}
		request := ipc.Request{Action: "set", Text: args[0]}
		if len(args) == 2 {
			budget, err := time.ParseDuration(args[1])
			if err != nil || budget <= 0 {
				return fmt.Errorf("budget must be a positive Go-style duration (for example 45m)")
			}
			request.Budget = args[1]
		}
		if err := send(request); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Focused: %s\n", args[0])
		return nil
	},
}

// Flag parsing stops at the text to accept negative positional budgets. Keep
// trailing help flags working, while an explicit -- still makes them literal.
func setHelpRequested(cmd *cobra.Command, args []string) bool {
	if cmd.ArgsLenAtDash() >= 0 {
		return false
	}
	for i, arg := range args {
		if i > 0 && (arg == "--help" || arg == "-h") {
			return true
		}
	}
	return false
}

var doneCmd = &cobra.Command{
	Use:     "done",
	Aliases: []string{"clear"},
	Short:   "Complete and clear the current focus",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := send(ipc.Request{Action: "done"}); err != nil {
			return err
		}
		fmt.Println("Focus cleared.")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current focus state",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		response, err := ipc.Send(ipc.Request{Action: "status"})
		if err != nil {
			return err
		}
		if !response.OK {
			return fmt.Errorf("%s", response.Error)
		}
		out := cmd.OutOrStdout()
		if response.Status != nil && response.Status.ConfigError != "" {
			fmt.Fprintf(out, "config error: %s (running with previous config)\n", response.Status.ConfigError)
		}
		if response.Status == nil || response.Status.Text == "" {
			fmt.Fprintln(out, "No focus set.")
			return nil
		}
		status := response.Status
		elapsed := time.Duration(status.ElapsedSeconds) * time.Second
		fmt.Fprintf(out, "Focus:   %s\n", status.Text)
		fmt.Fprintf(out, "Elapsed: %s\n", shortDuration(elapsed))
		if status.Budget > 0 {
			fmt.Fprintf(out, "Budget:  %s\n", status.Budget)
			if status.Overage > 0 {
				fmt.Fprintf(out, "Overage: +%s\n", status.Overage)
			}
		}
		fmt.Fprintf(out, "Rung:    %d\n", status.Rung)
		if status.Paused {
			if status.PausedUntil != nil {
				fmt.Fprintf(out, "Paused:  until %s (%s remaining)\n", status.PausedUntil.Local().Format("3:04 PM"), shortDuration(time.Until(*status.PausedUntil)))
			} else {
				fmt.Fprintln(out, "Paused:  yes")
			}
		} else {
			fmt.Fprintln(out, "Paused:  no")
		}
		return nil
	},
}

var pauseCmd = &cobra.Command{
	Use:   "pause <duration>",
	Short: "Hide the HUD and pause reminders",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		duration, err := time.ParseDuration(args[0])
		if err != nil || duration <= 0 {
			return fmt.Errorf("duration must be a positive Go-style duration (for example 45m)")
		}
		if err := send(ipc.Request{Action: "pause", Duration: args[0]}); err != nil {
			return err
		}
		fmt.Printf("Paused for %s.\n", shortDuration(duration))
		return nil
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume reminders",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := send(ipc.Request{Action: "resume"}); err != nil {
			return err
		}
		fmt.Println("Resumed.")
		return nil
	},
}

var ackDrifted bool

var ackCmd = &cobra.Command{
	Use:   "ack",
	Short: "Acknowledge the current reminder",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		kind := "on_task"
		if ackDrifted {
			kind = "drifted"
		}
		if err := send(ipc.Request{Action: "ack", Kind: kind}); err != nil {
			return err
		}
		fmt.Printf("Acknowledged: %s.\n", kind)
		return nil
	},
}

func init() {
	// Once text begins, treat the remaining token as a positional budget so
	// negative durations get the same useful validation error as other inputs.
	setCmd.Flags().SetInterspersed(false)
	ackCmd.Flags().BoolVar(&ackDrifted, "drifted", false, "record that you had drifted")
	rootCmd.AddCommand(setCmd, doneCmd, statusCmd, pauseCmd, resumeCmd, ackCmd)
}

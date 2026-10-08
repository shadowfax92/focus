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
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		cmd.Printf("Focused: %s\n", args[0])
		return nil
	},
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
		if response.Status == nil || response.Status.Text == "" {
			cmd.Println("No focus set.")
			return nil
		}
		status := response.Status
		elapsed := time.Duration(status.ElapsedSeconds) * time.Second
		cmd.Printf("Focus:   %s\n", status.Text)
		cmd.Printf("Elapsed: %s\n", shortDuration(elapsed))
		if status.Budget > 0 {
			cmd.Printf("Budget:  %s\n", status.Budget)
			if elapsed > status.Budget {
				cmd.Printf("Overage: +%s\n", elapsed-status.Budget)
			}
		}
		cmd.Printf("Rung:    %d\n", status.Rung)
		if status.Paused {
			if status.PausedUntil != nil {
				cmd.Printf("Paused:  until %s (%s remaining)\n", status.PausedUntil.Local().Format("3:04 PM"), shortDuration(time.Until(*status.PausedUntil)))
			} else {
				cmd.Println("Paused:  yes")
			}
		} else {
			cmd.Println("Paused:  no")
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

package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/ipc"
)

var rootCmd = &cobra.Command{
	Use:          "focus",
	Short:        "A floating focus HUD with escalating reminders and distraction stats",
	SilenceUsage: true,
}

func Execute() error { return rootCmd.Execute() }

func send(request ipc.Request) error {
	response, err := ipc.Send(request)
	if err != nil {
		return err
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "daemon rejected the request"
		}
		return fmt.Errorf("%s", response.Error)
	}
	return nil
}

func shortDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration < time.Minute {
		return duration.Round(time.Second).String()
	}
	return duration.Round(time.Minute).String()
}

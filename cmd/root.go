package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "focus",
	Short: "A floating focus HUD with escalating reminders and distraction stats",
}

func Execute() error { return rootCmd.Execute() }

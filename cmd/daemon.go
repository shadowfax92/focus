package cmd

import (
	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/config"
	"github.com/shadowfax92/focus/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the focus daemon in the foreground",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.EnsureDefault(); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return daemon.Run(cfg)
	},
}

func init() { rootCmd.AddCommand(daemonCmd) }

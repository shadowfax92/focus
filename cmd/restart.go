package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the installed Focus launchd service",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(plistPath()); os.IsNotExist(err) {
			return fmt.Errorf("Focus service is not installed (run `focus install`)")
		} else if err != nil {
			return fmt.Errorf("check Focus service: %w", err)
		}
		target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + plistLabel
		if output, err := exec.Command("launchctl", "print", target).CombinedOutput(); err != nil {
			return fmt.Errorf("Focus service is not loaded (run `focus install`): %w: %s", err, strings.TrimSpace(string(output)))
		}
		// Kickstart replaces only the process. Persisted focus, budget, pause,
		// position, and history stay owned by the existing state/store files.
		if output, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); err != nil {
			return fmt.Errorf("restart Focus: %w: %s", err, strings.TrimSpace(string(output)))
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Restarted Focus.")
		return nil
	},
}

func init() { rootCmd.AddCommand(restartCmd) }

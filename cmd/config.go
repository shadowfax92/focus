package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	focusconfig "github.com/shadowfax92/focus/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the resolved configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := focusconfig.Load()
		if err != nil {
			return err
		}
		contents, err := focusconfig.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(contents))
		return nil
	},
}

var quotesCmd = &cobra.Command{
	Use:   "quotes",
	Short: "Manage takeover quotes",
}

var quotesAddCmd = &cobra.Command{
	Use:   "add <quote>",
	Short: "Add a takeover quote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := focusconfig.Load()
		if err != nil {
			return err
		}
		cfg.Quotes = append(cfg.Quotes, args[0])
		if err := focusconfig.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Added quote %d.\n", len(cfg.Quotes))
		return nil
	},
}

var quotesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List takeover quotes",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := focusconfig.Load()
		if err != nil {
			return err
		}
		if len(cfg.Quotes) == 0 {
			fmt.Println("No quotes configured.")
			return nil
		}
		for i, quote := range cfg.Quotes {
			fmt.Printf("%d. %s\n", i+1, quote)
		}
		return nil
	},
}

var quotesRemoveCmd = &cobra.Command{
	Use:   "rm <number>",
	Short: "Remove a takeover quote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		index, err := strconv.Atoi(args[0])
		if err != nil || index < 1 {
			return fmt.Errorf("quote number must be a positive integer")
		}
		cfg, err := focusconfig.Load()
		if err != nil {
			return err
		}
		if index > len(cfg.Quotes) {
			return fmt.Errorf("quote %d does not exist", index)
		}
		cfg.Quotes = append(cfg.Quotes[:index-1], cfg.Quotes[index:]...)
		if err := focusconfig.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("Removed quote %d.\n", index)
		return nil
	},
}

func init() {
	quotesCmd.AddCommand(quotesAddCmd, quotesListCmd, quotesRemoveCmd)
	rootCmd.AddCommand(configCmd, quotesCmd)
}

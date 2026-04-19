package cli

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var updateCollection string

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Scan filesystem and update index",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Update(updateCollection)
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Status()
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Drop all data and rebuild index from scratch (keeps collections)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Rebuild()
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVarP(&updateCollection, "collection", "c", "", "update specific collection only")
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(rebuildCmd)
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	collectionName string
	collectionMask string
)

var collectionCmd = &cobra.Command{
	Use:   "collection",
	Short: "Manage collections",
}

var collectionAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if collectionName == "" {
			return fmt.Errorf("--name is required")
		}

		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}

		mask := collectionMask
		if mask == "" {
			mask = "**/*.md"
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.CollectionAdd(absPath, collectionName, mask)
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

var collectionRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.CollectionRemove(args[0])
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

var collectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all collections",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.CollectionList()
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

var collectionRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a collection",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.CollectionRename(args[0], args[1])
		if err != nil {
			return err
		}

		fmt.Print(string(body))
		return nil
	},
}

func init() {
	collectionAddCmd.Flags().StringVar(&collectionName, "name", "", "collection name (required)")
	collectionAddCmd.Flags().StringVar(&collectionMask, "mask", "**/*.md", "file glob pattern")

	collectionCmd.AddCommand(collectionAddCmd)
	collectionCmd.AddCommand(collectionRemoveCmd)
	collectionCmd.AddCommand(collectionListCmd)
	collectionCmd.AddCommand(collectionRenameCmd)
	rootCmd.AddCommand(collectionCmd)
}

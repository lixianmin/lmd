package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/logo"
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

		if err := dao.AddCollection(collectionName, absPath, mask, nil); err != nil {
			return err
		}

		logo.Info("collection add: name=%s path=%s mask=%s", collectionName, absPath, mask)
		fmt.Printf("Collection '%s' added: %s\n", collectionName, absPath)
		return nil
	},
}

var collectionRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := dao.RemoveCollection(args[0]); err != nil {
			return err
		}

		logo.Info("collection remove: name=%s", args[0])
		fmt.Printf("Collection '%s' removed\n", args[0])
		return nil
	},
}

var collectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all collections",
	RunE: func(cmd *cobra.Command, args []string) error {
		cols, err := dao.ListCollections()
		if err != nil {
			return err
		}

		if len(cols) == 0 {
			fmt.Println("No collections found.")
			return nil
		}

		for _, c := range cols {
			fmt.Printf("%s\t%s\t(%d docs)\t%s\n", c.Name, c.Path, c.DocCount, c.GlobPattern)
		}
		return nil
	},
}

var collectionRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a collection",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := dao.RenameCollection(args[0], args[1]); err != nil {
			return err
		}

		logo.Info("collection rename: %s -> %s", args[0], args[1])
		fmt.Printf("Collection renamed: %s -> %s\n", args[0], args[1])
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

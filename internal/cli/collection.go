package cli

import (
	"encoding/json"
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
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}

		if collectionName == "" {
			collectionName = filepath.Base(absPath)
		}

		mask := collectionMask
		if mask == "" {
			mask = "**/*.{md,txt}"
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.CollectionAdd(absPath, collectionName, mask)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			Mask   string `json:"mask"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Added collection %q: path=%s mask=%s\n", resp.Name, resp.Path, resp.Mask)
		fmt.Println("Indexing in background. Use `lmd status` to check progress.")
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

		if jsonOut {
			printBody(body)
			return nil
		}

		fmt.Printf("Removed collection %q\n", args[0])
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

		if jsonOut {
			printBody(body)
			return nil
		}

		var cols []struct {
			Name     string   `json:"name"`
			Path     string   `json:"path"`
			Glob     string   `json:"glob"`
			DocCount int      `json:"doc_count"`
			Ignore   []string `json:"ignore,omitempty"`
		}
		if err := json.Unmarshal(body, &cols); err != nil {
			fmt.Print(string(body))
			return nil
		}

		if len(cols) == 0 {
			fmt.Println("No collections.")
			return nil
		}

		fmt.Printf("%-15s %8s %-12s %s\n", "COLLECTION", "DOCS", "GLOB", "PATH")
		for _, c := range cols {
			fmt.Printf("%-15s %8d %-12s %s\n", c.Name, c.DocCount, c.Glob, c.Path)
		}
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

		if jsonOut {
			printBody(body)
			return nil
		}

		fmt.Printf("Renamed %q -> %q\n", args[0], args[1])
		return nil
	},
}

func init() {
	collectionAddCmd.Flags().StringVar(&collectionName, "name", "", "collection name (defaults to directory name)")
	collectionAddCmd.Flags().StringVar(&collectionMask, "mask", "**/*.{md,txt}", "file glob pattern")

	collectionCmd.AddCommand(collectionAddCmd)
	collectionCmd.AddCommand(collectionRemoveCmd)
	collectionCmd.AddCommand(collectionListCmd)
	collectionCmd.AddCommand(collectionRenameCmd)
	rootCmd.AddCommand(collectionCmd)
}

package cli

import (
	"fmt"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/logo"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage path context metadata",
}

var contextAddCmd = &cobra.Command{
	Use:   "add <collection/path> <description>",
	Short: "Add or update context for a path",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		collection, p := parseContextPath(args[0])
		logo.Info("context add: %s/%s", collection, p)
		return store.AddContext(db, collection, p, args[1])
	},
}

var contextRemoveCmd = &cobra.Command{
	Use:   "remove <collection/path>",
	Short: "Remove context for a path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		collection, p := parseContextPath(args[0])
		logo.Info("context remove: %s/%s", collection, p)
		return store.RemoveContext(db, collection, p)
	},
}

var contextListCmd = &cobra.Command{
	Use:   "list <collection>",
	Short: "List contexts for a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		contexts, err := store.ListContexts(db, args[0])
		if err != nil {
			return err
		}
		if len(contexts) == 0 {
			fmt.Println("No contexts found.")
			return nil
		}
		for _, c := range contexts {
			if c.Path == "" {
				fmt.Printf("  lmd://%s (collection-level)\n", c.Collection)
			} else {
				fmt.Printf("  lmd://%s/%s\n", c.Collection, c.Path)
			}
			fmt.Printf("    %s\n", c.Context)
		}
		return nil
	},
}

func parseContextPath(uri string) (collection, p string) {
	uri = strings.TrimPrefix(uri, "lmd://")
	parts := strings.SplitN(uri, "/", 2)
	collection = parts[0]
	if len(parts) > 1 {
		p = parts[1]
	}
	return
}

func init() {
	contextCmd.AddCommand(contextAddCmd)
	contextCmd.AddCommand(contextRemoveCmd)
	contextCmd.AddCommand(contextListCmd)
	rootCmd.AddCommand(contextCmd)
}

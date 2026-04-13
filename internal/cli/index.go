package cli

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var updateCollection string

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Scan filesystem and update index",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		idx := service.NewIndexer(db, tok)

		cols, err := store.ListCollections(db)
		if err != nil {
			return err
		}

		totalIndexed := 0
		totalUpdated := 0
		totalUnchanged := 0
		totalRemoved := 0

		for _, col := range cols {
			if updateCollection != "" && col.Name != updateCollection {
				continue
			}

			result, err := idx.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
			if err != nil {
				fmt.Printf("Error indexing %s: %v\n", col.Name, err)
				continue
			}

			fmt.Printf("%s: indexed=%d updated=%d unchanged=%d removed=%d\n",
				col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
			totalIndexed += result.Indexed
			totalUpdated += result.Updated
			totalUnchanged += result.Unchanged
			totalRemoved += result.Removed
		}

		fmt.Printf("\nTotal: indexed=%d updated=%d unchanged=%d removed=%d\n",
			totalIndexed, totalUpdated, totalUnchanged, totalRemoved)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		cols, err := store.ListCollections(db)
		if err != nil {
			return err
		}

		fmt.Printf("Database: %s\n\n", getDefaultIndexPath())
		if len(cols) == 0 {
			fmt.Println("No collections.")
			return nil
		}

		for _, c := range cols {
			fmt.Printf("  %s\n", c.Name)
			fmt.Printf("    Path:  %s\n", c.Path)
			fmt.Printf("    Glob:  %s\n", c.GlobPattern)
			fmt.Printf("    Docs:  %d\n", c.DocCount)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVarP(&updateCollection, "collection", "c", "", "update specific collection only")
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(statusCmd)
}

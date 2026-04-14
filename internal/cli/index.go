package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
	"github.com/spf13/cobra"
)

var updateCollection string

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Scan filesystem and update index",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		logo.Info("update: starting")

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		idx := service.NewIndexer(tok)

		cols, err := dao.ListCollections()
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
				logo.Error("update: %s failed: %s", col.Name, err)
				fmt.Printf("Error indexing %s: %v\n", col.Name, err)
				continue
			}

			logo.Info("update: %s +%d ~%d =%d -%d", col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
			fmt.Printf("%s: indexed=%d updated=%d unchanged=%d removed=%d\n",
				col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
			totalIndexed += result.Indexed
			totalUpdated += result.Updated
			totalUnchanged += result.Unchanged
			totalRemoved += result.Removed
		}

		fmt.Printf("\nTotal: indexed=%d updated=%d unchanged=%d removed=%d\n",
			totalIndexed, totalUpdated, totalUnchanged, totalRemoved)
		logo.Info("update: done indexed=%d updated=%d unchanged=%d removed=%d elapsed=%s",
			totalIndexed, totalUpdated, totalUnchanged, totalRemoved, time.Since(start))
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cols, err := dao.ListCollections()
		if err != nil {
			return err
		}

		fmt.Printf("Database: %s\n\n", getDefaultIndexPath())
		if len(cols) == 0 {
			fmt.Println("No collections.")
			return nil
		}

		totalDocs := 0
		for _, c := range cols {
			fmt.Printf("  %s\n", c.Name)
			fmt.Printf("    Path:  %s\n", c.Path)
			fmt.Printf("    Glob:  %s\n", c.GlobPattern)
			fmt.Printf("    Docs:  %d\n", c.DocCount)
			totalDocs += c.DocCount
		}

		chunkCount, embedCount := dao.GetChunkCounts()

		fmt.Printf("\n  Total: %d documents, %d chunks, %d embedded\n", totalDocs, chunkCount, embedCount)
		logo.Info("status: docs=%d chunks=%d embedded=%d", totalDocs, chunkCount, embedCount)
		if chunkCount > 0 && embedCount < chunkCount {
			fmt.Printf("  ⚠ %d chunks pending embedding\n", chunkCount-embedCount)
		}
		return nil
	},
}

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Drop all data and rebuild index from scratch (keeps collections)",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		logo.Info("rebuild: starting")

		cols, err := dao.ListCollections()
		if err != nil {
			return err
		}
		if len(cols) == 0 {
			fmt.Println("No collections to reindex.")
			return nil
		}

		dao.DB.Close()

		dbPath := getDefaultIndexPath()
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove database: %w", err)
		}
		fmt.Println("Database reset.")

		if err := dao.Init(dbPath); err != nil {
			return err
		}

		for _, col := range cols {
			if err := dao.AddCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns); err != nil {
				fmt.Printf("Warning: failed to restore collection %s: %v\n", col.Name, err)
			}
		}

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}
		idx := service.NewIndexer(tok)

		for _, col := range cols {
			result, err := idx.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
			if err != nil {
				fmt.Printf("Error indexing %s: %v\n", col.Name, err)
				continue
			}
			fmt.Printf("%s: indexed=%d updated=%d unchanged=%d removed=%d\n",
				col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
		}

		provider := newProvider()
		defer provider.Close()
		embedder := service.NewEmbedder(provider)
		embedResult, err := embedder.EmbedBatch(context.Background(), 0)
		if err != nil {
			return fmt.Errorf("embedding failed: %w", err)
		}
		fmt.Printf("\nEmbedded %d chunks, skipped %d\n", embedResult.Embedded, embedResult.Skipped)
		logo.Info("rebuild: done embedded=%d skipped=%d elapsed=%s", embedResult.Embedded, embedResult.Skipped, time.Since(start))
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVarP(&updateCollection, "collection", "c", "", "update specific collection only")
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(rebuildCmd)
}

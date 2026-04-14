package cli

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var (
	searchCollection string
	searchLimit      int
	searchFull       bool
	searchMinScore   float64
	outputFormat     string
)

func newProvider() *embedding.GGUFProvider {
	return embedding.NewGGUFProvider(embedding.DefaultModelPath())
}

func syncIndex(db *sql.DB) {
	cols, err := store.ListCollections(db)
	if err != nil || len(cols) == 0 {
		return
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: index sync skipped: %v\n", err)
		return
	}

	idx := service.NewIndexer(db, tok)
	anyChange := false
	for _, col := range cols {
		result, err := idx.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: index sync %s failed: %v\n", col.Name, err)
			continue
		}
		if result.Indexed > 0 || result.Updated > 0 || result.Removed > 0 {
			if !anyChange {
				fmt.Fprintf(os.Stderr, "Syncing index...\n")
				anyChange = true
			}
			fmt.Fprintf(os.Stderr, "  %s: +%d ~%d -%d\n", col.Name, result.Indexed, result.Updated, result.Removed)
		}
	}
}

func syncEmbeddings(db *sql.DB) {
	provider := newProvider()
	defer provider.Close()
	embedder := service.NewEmbedder(db, provider)
	result, err := embedder.EmbedAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: embed sync failed: %v\n", err)
		return
	}
	if result.Embedded > 0 {
		fmt.Fprintf(os.Stderr, "Embedded %d new chunks\n", result.Embedded)
	}
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "BM25 keyword search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		syncIndex(db)

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		searcher := service.NewSearcher(db, tok)
		results, err := searcher.SearchLex(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		return formatResults(os.Stdout, results)
	},
}

var vsearchCmd = &cobra.Command{
	Use:   "vsearch <query>",
	Short: "Vector semantic search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		syncIndex(db)
		syncEmbeddings(db)

		provider := newProvider()
		defer provider.Close()
		searcher := service.NewSearcher(db, nil)
		results, err := searcher.SearchVector(provider, args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		return formatResults(os.Stdout, results)
	},
}

var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Hybrid search (BM25 + vector with RRF fusion)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		syncIndex(db)
		syncEmbeddings(db)

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		provider := newProvider()
		defer provider.Close()
		searcher := service.NewSearcher(db, tok)
		results, err := searcher.SearchHybrid(provider, args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		return formatResults(os.Stdout, results)
	},
}

func formatResults(w *os.File, hits []formatter.SearchHit) error {
	switch outputFormat {
	case "json":
		return formatter.NewJSONFormatter().Format(w, hits)
	case "md", "markdown":
		return formatter.NewMarkdownFormatter().Format(w, hits)
	case "csv":
		return formatter.NewCSVFormatter().Format(w, hits)
	default:
		return formatter.NewTextFormatter(formatter.TextConfig{Full: searchFull}).Format(w, hits)
	}
}

func init() {
	searchCmd.Flags().StringVarP(&searchCollection, "collection", "c", "", "search in specific collection")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 5, "number of results")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")
	searchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, json, md, csv")

	vsearchCmd.Flags().AddFlagSet(searchCmd.Flags())
	queryCmd.Flags().AddFlagSet(searchCmd.Flags())

	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(vsearchCmd)
	rootCmd.AddCommand(queryCmd)
}

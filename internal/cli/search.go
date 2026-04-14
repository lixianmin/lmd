package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
	"github.com/spf13/cobra"
)

var (
	searchCollection string
	searchLimit      int
	searchFull       bool
	searchMinScore   float64
	outputJSON       bool
	outputFormat     string
)

func newProvider() *embedding.GGUFProvider {
	if err := embedding.EnsureModel(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}
	return embedding.NewGGUFProvider(embedding.DefaultModelPath())
}

func syncIndex() {
	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		logo.Error("syncIndex: tokenizer init failed: %s", err)
		return
	}

	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("syncIndex: list collections failed: %s", err)
		return
	}

	idx := service.NewIndexer(tok)
	anyChange := false
	for _, col := range cols {
		result, err := idx.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("syncIndex: %s failed: %s", col.Name, err)
			fmt.Fprintf(os.Stderr, "Warning: index sync %s failed: %v\n", col.Name, err)
			continue
		}
		if result.Indexed > 0 || result.Updated > 0 || result.Removed > 0 {
			if !anyChange {
				fmt.Fprintf(os.Stderr, "Syncing index...\n")
				anyChange = true
			}
			fmt.Fprintf(os.Stderr, "  %s: +%d ~%d -%d\n", col.Name, result.Indexed, result.Updated, result.Removed)
			logo.Info("syncIndex: %s +%d ~%d -%d", col.Name, result.Indexed, result.Updated, result.Removed)
		}
	}
}

func syncEmbeddings() {
	unembeddedCount := dao.GetUnembeddedCount()
	if unembeddedCount == 0 {
		return
	}

	batchSize := 10
	if unembeddedCount < batchSize {
		batchSize = unembeddedCount
	}

	start := time.Now()
	fmt.Fprintf(os.Stderr, "Embedding %d/%d chunks...\n", batchSize, unembeddedCount)
	provider := newProvider()
	defer provider.Close()
	embedder := service.NewEmbedder(provider)
	result, err := embedder.EmbedBatch(context.Background(), batchSize)
	if err != nil {
		logo.Error("syncEmbeddings failed: %s", err)
		fmt.Fprintf(os.Stderr, "Warning: embed sync failed: %v\n", err)
		return
	}
	if result.Embedded > 0 {
		fmt.Fprintf(os.Stderr, "  Embedded %d chunks in %s (%d remaining)\n",
			result.Embedded, time.Since(start).Round(time.Second), unembeddedCount-result.Embedded)
	}
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "BM25 keyword search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		syncIndex()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		searcher := service.NewSearcher(tok)
		results, err := searcher.SearchLex(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		logo.Info("search: query=%q collection=%s limit=%d results=%d", args[0], searchCollection, searchLimit, len(results))
		return formatResults(os.Stdout, results)
	},
}

var vsearchCmd = &cobra.Command{
	Use:   "vsearch <query>",
	Short: "Vector semantic search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		syncIndex()
		syncEmbeddings()

		provider := newProvider()
		defer provider.Close()
		searcher := service.NewSearcher(nil)

		minScore := searchMinScore
		if !cmd.Flags().Changed("min-score") {
			minScore = 0.3
		}

		results, err := searcher.SearchVector(provider, args[0], searchCollection, searchLimit, minScore)
		if err != nil {
			return err
		}

		logo.Info("vsearch: query=%q collection=%s limit=%d results=%d", args[0], searchCollection, searchLimit, len(results))
		return formatResults(os.Stdout, results)
	},
}

var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Hybrid search (BM25 + vector with RRF fusion)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		syncIndex()
		syncEmbeddings()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		provider := newProvider()
		defer provider.Close()
		searcher := service.NewSearcher(tok)
		results, err := searcher.SearchHybrid(provider, args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		logo.Info("query: query=%q collection=%s limit=%d results=%d", args[0], searchCollection, searchLimit, len(results))
		return formatResults(os.Stdout, results)
	},
}

func formatResults(w *os.File, hits []formatter.SearchHit) error {
	if outputJSON {
		return formatter.NewJSONFormatter().Format(w, hits)
	}
	switch outputFormat {
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
	searchCmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	searchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, md, csv")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")

	vsearchCmd.Flags().AddFlagSet(searchCmd.Flags())
	queryCmd.Flags().AddFlagSet(searchCmd.Flags())

	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(vsearchCmd)
	rootCmd.AddCommand(queryCmd)
}

package cli

import (
	"fmt"
	"os"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/service"
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

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		provider := embedding.NewMockProvider(1024)
		searcher := service.NewSearcher(db, tok)
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

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		provider := embedding.NewMockProvider(1024)
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
	default:
		return formatter.NewTextFormatter(formatter.TextConfig{Full: searchFull}).Format(w, hits)
	}
}

func init() {
	searchCmd.Flags().StringVarP(&searchCollection, "collection", "c", "", "search in specific collection")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 5, "number of results")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")
	searchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, json")

	vsearchCmd.Flags().AddFlagSet(searchCmd.Flags())
	queryCmd.Flags().AddFlagSet(searchCmd.Flags())

	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(vsearchCmd)
	rootCmd.AddCommand(queryCmd)
}

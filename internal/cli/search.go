package cli

import (
	"fmt"

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

		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		for _, r := range results {
			fmt.Printf("%s:%d #%s\n", r.Path, r.Line, r.DocID)
			fmt.Printf("Title: %s\n", r.Title)
			fmt.Printf("Score: %.0f%%\n", r.Score*100)
			if searchFull {
				fmt.Println()
				doc, err := store.GetDocumentByDocID(db, r.DocID)
				if err == nil {
					fmt.Println(doc.Body)
				} else {
					fmt.Println(r.Snippet)
				}
			} else {
				fmt.Printf("\n%s\n", r.Snippet)
			}
			fmt.Println()
		}
		return nil
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchCollection, "collection", "c", "", "search in specific collection")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 5, "number of results")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")
	rootCmd.AddCommand(searchCmd)
}

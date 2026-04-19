package cli

import (
	"encoding/json"
	"os"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/lixianmin/lmd/internal/formatter"
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
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Search(args[0], searchCollection, searchLimit, searchMinScore, outputFormat, jsonOut)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		return formatResponse(body)
	},
}

var vsearchCmd = &cobra.Command{
	Use:   "vsearch <query>",
	Short: "Vector semantic search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		minScore := searchMinScore
		if !cmd.Flags().Changed("min-score") {
			minScore = 0.3
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.VSearch(args[0], searchCollection, searchLimit, minScore)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		return formatResponse(body)
	},
}

var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Hybrid search (BM25 + vector + HyDE)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Query(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		return formatResponse(body)
	},
}

func formatResponse(body []byte) error {
	var resp struct {
		Hits []formatter.SearchHit `json:"hits"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}

	return formatResults(os.Stdout, resp.Hits)
}

func formatResults(w *os.File, hits []formatter.SearchHit) error {
	if jsonOut {
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
	searchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, md, csv")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")

	vsearchCmd.Flags().AddFlagSet(searchCmd.Flags())
	queryCmd.Flags().AddFlagSet(searchCmd.Flags())

	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(vsearchCmd)
	rootCmd.AddCommand(queryCmd)
}

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Status()
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			Database     string `json:"database"`
			Documents    int    `json:"documents"`
			Chunks       int    `json:"chunks"`
			Embedded     int    `json:"embedded"`
			Pending      int    `json:"pending"`
			ETA          string `json:"eta"`
		HydeTotal int    `json:"hyde_total"`
		HydeDone  int    `json:"hyde_done"`
		Collections []struct {
				Name       string `json:"name"`
				Path       string `json:"path"`
				Glob       string `json:"glob"`
				DocCount   int    `json:"doc_count"`
				ChunkCount int    `json:"chunk_count"`
			} `json:"collections"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Database:   %s\n", resp.Database)
		fmt.Printf("Documents:  %d\n", resp.Documents)
		fmt.Printf("Chunks:     %d\n", resp.Chunks)
		fmt.Printf("Embedded:   %d/%d\n", resp.Embedded, resp.Chunks)
		if resp.Pending > 0 && resp.ETA != "" {
			fmt.Printf("Embed ETA:  %s\n", resp.ETA)
		}
		fmt.Printf("HyDE:       %d/%d\n", resp.HydeDone, resp.HydeTotal)
		if len(resp.Collections) > 0 {
			fmt.Println()
			fmt.Printf("%-15s %8s %8s %s\n", "COLLECTION", "DOCS", "CHUNKS", "PATH")
			for _, c := range resp.Collections {
				fmt.Printf("%-15s %8d %8d %s\n", c.Name, c.DocCount, c.ChunkCount, c.Path)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

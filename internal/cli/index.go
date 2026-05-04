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
			Database    string `json:"database"`
			Documents   int    `json:"documents"`
			Chunks      int    `json:"chunks"`
			Embedded    int    `json:"embedded"`
			Pending     int    `json:"pending"`
			Collections []struct {
				Name     string `json:"name"`
				Path     string `json:"path"`
				Glob     string `json:"glob"`
				DocCount int    `json:"doc_count"`
			} `json:"collections"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Database:   %s\n", resp.Database)
		fmt.Printf("Documents:  %d\n", resp.Documents)
		fmt.Printf("Chunks:     %d\n", resp.Chunks)
		fmt.Printf("Embedded:   %d\n", resp.Embedded)
		fmt.Printf("Pending:    %d\n", resp.Pending)
		if len(resp.Collections) > 0 {
			fmt.Println()
fmt.Printf("%-15s %8s %s\n", "COLLECTION", "DOCS", "PATH")
		for _, c := range resp.Collections {
			fmt.Printf("%-15s %8d %s\n", c.Name, c.DocCount, c.Path)
			}
		}
		return nil
	},
}

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Drop all data and rebuild index from scratch (keeps collections)",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Rebuild()
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			Collections int    `json:"collections"`
			Elapsed     string `json:"elapsed"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Rebuild complete: collections=%d elapsed=%s\n", resp.Collections, resp.Elapsed)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(rebuildCmd)
}

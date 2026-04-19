package cli

import (
	"encoding/json"
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var updateCollection string

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Scan filesystem and update index",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Update(updateCollection)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			Collections []struct {
				Name      string `json:"name"`
				Indexed   int    `json:"indexed"`
				Updated   int    `json:"updated"`
				Unchanged int    `json:"unchanged"`
				Removed   int    `json:"removed"`
			} `json:"collections"`
			Totals struct {
				Indexed   int `json:"indexed"`
				Updated   int `json:"updated"`
				Unchanged int `json:"unchanged"`
				Removed   int `json:"removed"`
			} `json:"totals"`
			Elapsed string `json:"elapsed"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		for _, c := range resp.Collections {
			fmt.Printf("  %s: +%d ~%d =%d -%d\n", c.Name, c.Indexed, c.Updated, c.Unchanged, c.Removed)
		}
		fmt.Printf("  totals: indexed=%d updated=%d unchanged=%d removed=%d elapsed=%s\n",
			resp.Totals.Indexed, resp.Totals.Updated, resp.Totals.Unchanged, resp.Totals.Removed, resp.Elapsed)
		return nil
	},
}

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
			fmt.Printf("%-15s %-8s %s\n", "COLLECTION", "DOCS", "PATH")
			for _, c := range resp.Collections {
				fmt.Printf("%-15s %-8d %s\n", c.Name, c.DocCount, c.Path)
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
			Indexed int    `json:"indexed"`
			Skipped int    `json:"skipped"`
			Elapsed string `json:"elapsed"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Rebuild complete: indexed=%d skipped=%d elapsed=%s\n", resp.Indexed, resp.Skipped, resp.Elapsed)
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVarP(&updateCollection, "collection", "c", "", "update specific collection only")
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(rebuildCmd)
}

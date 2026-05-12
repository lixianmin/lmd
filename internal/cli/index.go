package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

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
			Rebuild      *struct {
				Status    string `json:"status"`
				Total     string `json:"total"`
				Processed string `json:"processed"`
				Errors    string `json:"errors"`
			} `json:"rebuild"`
			Pipeline *struct {
				Status    string `json:"status"`
				Total     string `json:"total"`
				Processed string `json:"processed"`
				Errors    string `json:"errors"`
			} `json:"pipeline"`
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
		if resp.Rebuild != nil && resp.Rebuild.Status == "running" {
			fmt.Println()
			total, _ := strconv.Atoi(resp.Rebuild.Total)
			processed, _ := strconv.Atoi(resp.Rebuild.Processed)
			errors, _ := strconv.Atoi(resp.Rebuild.Errors)
			pct := float64(0)
			if total > 0 {
				pct = float64(processed) / float64(total) * 100
			}
			fmt.Printf("Rebuild:    %d/%d (%.1f%%) errors=%d\n", processed, total, pct, errors)
		}
		if resp.Pipeline != nil && resp.Pipeline.Status == "running" {
			fmt.Println()
			total, _ := strconv.Atoi(resp.Pipeline.Total)
			processed, _ := strconv.Atoi(resp.Pipeline.Processed)
			errors, _ := strconv.Atoi(resp.Pipeline.Errors)
			pct := float64(0)
			if total > 0 {
				pct = float64(processed) / float64(total) * 100
			}
			fmt.Printf("Pipeline:   %d/%d (%.1f%%) errors=%d\n", processed, total, pct, errors)
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
			Status      string `json:"status"`
			Collections int    `json:"collections"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Rebuild started: collections=%d (processing in background)\n", resp.Collections)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(rebuildCmd)
}

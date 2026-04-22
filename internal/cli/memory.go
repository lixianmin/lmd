package cli

import (
	"fmt"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	memoryType  string
	memoryLimit int
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Agent memory operations",
}

var memoryAddCmd = &cobra.Command{
	Use:   "add <content>",
	Short: "Add a memory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.MemoryAdd(args[0], memoryType)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			ID        int64  `json:"id"`
			Type      string `json:"type"`
			CreatedAt string `json:"created_at"`
		}
		if err := convert.FromJsonE(body, &resp); err != nil {
			return err
		}

		fmt.Printf("id=%d type=%s created_at=%s\n", resp.ID, resp.Type, resp.CreatedAt)
		return nil
	},
}

var memoryQueryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Query memories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.MemoryQuery(args[0], memoryLimit)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var results []struct {
			ID        int64   `json:"id"`
			Content   string  `json:"content"`
			Type      string  `json:"type"`
			Score     float64 `json:"score"`
			CreatedAt string  `json:"created_at"`
		}
		if err := convert.FromJsonE(body, &results); err != nil {
			return err
		}

		for _, r := range results {
			fmt.Printf("[%s] %.4f %s\n  %s\n\n", r.Type, r.Score, r.CreatedAt, r.Content)
		}
		return nil
	},
}

func init() {
	memoryAddCmd.Flags().StringVar(&memoryType, "type", "episode", "memory type: fact|episode|relation")

	memoryQueryCmd.Flags().IntVar(&memoryLimit, "limit", 10, "max results")

	memoryCmd.AddCommand(memoryAddCmd)
	memoryCmd.AddCommand(memoryQueryCmd)
	rootCmd.AddCommand(memoryCmd)
}

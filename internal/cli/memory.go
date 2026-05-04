package cli

import (
	"fmt"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
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
		body, err := client.MemoryAdd(args[0])
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			ID         int64  `json:"id"`
			Collection string `json:"collection"`
			CreatedAt  string `json:"created_at"`
		}
		if err := convert.FromJsonE(body, &resp); err != nil {
			return err
		}

		fmt.Printf("id=%d collection=%s created_at=%s\n", resp.ID, resp.Collection, resp.CreatedAt)
		return nil
	},
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a memory by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int64
		if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
			return fmt.Errorf("invalid id: %s", args[0])
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.MemoryDelete(id)
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		fmt.Printf("Deleted memory id=%d\n", id)
		return nil
	},
}

var memoryUpdateCmd = &cobra.Command{
	Use:   "update <id> <content>",
	Short: "Update a memory by ID",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int64
		if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
			return fmt.Errorf("invalid id: %s", args[0])
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.MemoryUpdate(id, args[1])
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		fmt.Printf("Updated memory id=%d\n", id)
		return nil
	},
}

func init() {
	memoryCmd.AddCommand(memoryAddCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)
	memoryCmd.AddCommand(memoryUpdateCmd)
	rootCmd.AddCommand(memoryCmd)
}

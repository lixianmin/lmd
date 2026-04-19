package cli

import (
	"encoding/json"
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Generate vector embeddings for indexed chunks",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Embed()
		if err != nil {
			return err
		}

		if jsonOut {
			printBody(body)
			return nil
		}

		var resp struct {
			Embedded int `json:"embedded"`
			Skipped  int `json:"skipped"`
			Failed   int `json:"failed"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		fmt.Printf("Embedded: %d  Skipped: %d  Failed: %d\n", resp.Embedded, resp.Skipped, resp.Failed)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(embedCmd)
}

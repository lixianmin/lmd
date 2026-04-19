package cli

import (
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

		fmt.Print(string(body))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(embedCmd)
}

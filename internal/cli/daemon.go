package cli

import (
	"context"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonDetach bool

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the LMD daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if daemonDetach {
			return daemon.StartBackground()
		}

		cfg := config.Cfg
		d := daemon.NewDaemon(cfg)
		ctx := context.Background()
		return d.Start(ctx)
	},
}

func init() {
	daemonCmd.Flags().BoolVar(&daemonDetach, "detach", false, "start daemon in background")
	rootCmd.AddCommand(daemonCmd)
}

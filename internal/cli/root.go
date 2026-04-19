package cli

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/spf13/cobra"
)

var (
	indexPath string
	verbose   bool
)

var rootCmd = &cobra.Command{
	Use:   "lmd",
	Short: "LMD - Local Markdown Docs search engine",
	Long:  "A local hybrid search engine for Markdown documents with Chinese language support.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if _, err := config.Load(); err != nil {
			return fmt.Errorf("config load failed: %w", err)
		}

		if cmd == daemonCmd {
			return nil
		}

		if err := dao.Init(config.Cfg.Database.Path); err != nil {
			return fmt.Errorf("dao init failed: %w", err)
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)
		if err := client.EnsureDaemon(); err != nil {
			return fmt.Errorf("daemon start failed: %w", err)
		}
		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&indexPath, "index", "", "database file path (default: ~/.cache/lmd/index.sqlite)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug-level logging")
	rootCmd.Version = "0.1.0"
}

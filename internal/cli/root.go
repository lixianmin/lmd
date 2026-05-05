package cli

import (
	"encoding/json"
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	jsonOut bool
)

var rootCmd = &cobra.Command{
	Use:   "lmd",
	Short: "LMD - Local Markdown Docs search engine",
	Long:  "A local hybrid search engine for Markdown documents with Chinese language support.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if _, err := config.Load(); err != nil {
			return fmt.Errorf("config load failed: %w", err)
		}

		if cmd == daemonStartCmd || cmd == stopCmd || cmd == benchLongMemEvalCmd {
			return nil
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
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
	rootCmd.Version = "0.1.0"
}

func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func printBody(body []byte) {
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		printJSON(v)
	} else {
		fmt.Print(string(body))
	}
}

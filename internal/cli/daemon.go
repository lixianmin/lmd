package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonStartCmd = &cobra.Command{
	Use:    "daemon-start",
	Short:  "Start daemon in foreground (internal)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Cfg
		d := daemon.NewDaemon(cfg)
		ctx := context.Background()
		return d.Start(ctx)
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running LMD daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Cfg
		if cfg == nil {
			cfg = config.DefaultConfig()
		}

		pidFile := daemon.PidPath()
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, err := strconv.Atoi(string(data)); err == nil {
				if proc, err := os.FindProcess(pid); err == nil {
					if proc.Signal(os.Interrupt) == nil {
						fmt.Printf("daemon (pid %d) stopped\n", pid)
						os.Remove(pidFile)
						return nil
					}
				}
			}
		}

		client := daemon.NewClient(cfg.Daemon.Port)
		if client.IsAlive() {
			return fmt.Errorf("daemon is alive on port %d but PID file is invalid; kill manually", cfg.Daemon.Port)
		}

		fmt.Println("daemon is not running")
		os.Remove(pidFile)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(daemonStartCmd)
	rootCmd.AddCommand(stopCmd)
}

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

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start or manage the LMD daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the LMD daemon (foreground)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Cfg
		d := daemon.NewDaemon(cfg)
		ctx := context.Background()
		return d.Start(ctx)
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running LMD daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Cfg
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		client := daemon.NewClient(cfg.Daemon.Port)
		if !client.IsAlive() {
			fmt.Println("daemon is not running")
			return nil
		}

		pidFile := daemon.PidPath()
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("cannot read pid file: %w", err)
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			return fmt.Errorf("invalid pid: %w", err)
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("cannot find process: %w", err)
		}
		if err := proc.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("cannot stop daemon: %w", err)
		}

		fmt.Printf("daemon (pid %d) stopped\n", pid)
		os.Remove(pidFile)
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	rootCmd.AddCommand(daemonCmd)
}

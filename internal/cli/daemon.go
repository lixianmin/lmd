package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

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
		pidFile := daemon.PidPath()

		if !daemon.IsRunning() {
			fmt.Println("daemon is not running")
			os.Remove(pidFile)
			return nil
		}

		data, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("cannot read lock file: %w", err)
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			return fmt.Errorf("invalid pid in lock file: %w", err)
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("cannot find process: %w", err)
		}
		if err := proc.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("cannot stop daemon: %w", err)
		}

		// Wait for daemon to release its lock (deterministic signal of graceful shutdown).
		// No fixed timeout — daemon removes PID file when Stop() completes.
		// User can Ctrl+C to abort the wait.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		t := time.NewTicker(1 * time.Second)
		defer t.Stop()

		for {
			if !daemon.IsRunning() {
				fmt.Printf("daemon (pid %d) stopped\n", pid)
				return nil
			}
			select {
			case <-t.C:
				os.Stderr.WriteString(".")
			case <-ctx.Done():
				fmt.Printf("\ndaemon (pid %d) is still stopping, it will exit on its own\n", pid)
				return nil
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(daemonStartCmd)
	rootCmd.AddCommand(stopCmd)
}

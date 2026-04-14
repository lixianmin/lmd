package main

import (
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/cli"
	"github.com/lixianmin/logo"
)

func main() {
	var log = createLogo()
	defer log.Close()

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

func createLogo() *logo.Logger {
	var log = logo.NewLogger()
	log.SetFuncCallDepth(5)
	log.AddFlag(logo.LogAsyncWrite)

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".cache", "lmd", "logs")

	const flag = logo.FlagDate | logo.FlagTime | logo.FlagShortFile | logo.FlagLevel
	var rollingFile = logo.NewRollingFileHook(
		logo.WithDirName(logDir),
		logo.WithFileNamePrefix(""),
		logo.WithHookFlag(flag),
	)
	log.AddHook(rollingFile)

	logo.SetLogger(log)
	return log
}

package main

import (
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/cli"
	"github.com/lixianmin/logo"
)

func main() {
	initLogo()
	defer logo.GetLogger().(*logo.Logger).Close()

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

func initLogo() {
	var log = logo.NewLogger()
	log.AddFlag(logo.LogAsyncWrite)

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".cache", "lmd", "logs")

	const flag = logo.FlagDate | logo.FlagTime | logo.FlagShortFile | logo.FlagLevel
	var rollingFile = logo.NewRollingFileHook(
		logo.WithDirName(logDir),
		logo.WithFileNamePrefix("lmd"),
		logo.WithHookFlag(flag),
	)
	log.AddHook(rollingFile)

	logo.SetLogger(log)
}

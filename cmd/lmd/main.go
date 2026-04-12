package main

import (
	"os"

	"github.com/lixianmin/lmd/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

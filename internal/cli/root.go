package cli

import (
	"database/sql"
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/store"
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
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&indexPath, "index", "", "database file path (default: ~/.cache/lmd/index.sqlite)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug-level logging")
	rootCmd.Version = "0.1.0"
}

func getDefaultIndexPath() string {
	if indexPath != "" {
		return indexPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "lmd.sqlite"
	}
	return filepath.Join(home, ".cache", "lmd", "index.sqlite")
}

func openDB() (*sql.DB, error) {
	return store.OpenAndInit(getDefaultIndexPath())
}

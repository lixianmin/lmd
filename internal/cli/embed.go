package cli

import (
	"context"
	"fmt"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/spf13/cobra"
)

var embedForce bool

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Generate vector embeddings for indexed chunks",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		provider := newProvider()
		defer provider.Close()
		embedder := service.NewEmbedder(db, provider)

		result, err := embedder.EmbedAll(context.Background())
		if err != nil {
			return err
		}

		fmt.Printf("Embedded: %d, Skipped: %d, Failed: %d\n",
			result.Embedded, result.Skipped, result.Failed)
		return nil
	},
}

func init() {
	embedCmd.Flags().BoolVar(&embedForce, "force", false, "re-embed everything")
	rootCmd.AddCommand(embedCmd)
}

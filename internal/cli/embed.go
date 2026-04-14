package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/logo"
	"github.com/spf13/cobra"
)

var embedForce bool

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Generate vector embeddings for indexed chunks",
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		logo.Info("embed: starting")

		provider := newProvider()
		defer provider.Close()
		embedder := service.NewEmbedder(provider)

		result, err := embedder.EmbedBatch(context.Background(), 0)
		if err != nil {
			return err
		}

		fmt.Printf("Embedded: %d, Skipped: %d, Failed: %d\n",
			result.Embedded, result.Skipped, result.Failed)
		logo.Info("embed: done embedded=%d skipped=%d failed=%d elapsed=%s",
			result.Embedded, result.Skipped, result.Failed, time.Since(start))
		return nil
	},
}

func init() {
	embedCmd.Flags().BoolVar(&embedForce, "force", false, "re-embed everything")
	rootCmd.AddCommand(embedCmd)
}

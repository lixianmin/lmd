package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var benchFixture string
var benchAllBackends bool

var benchCmd = &cobra.Command{
	Use:   "bench <fixture.json>",
	Short: "Run search quality benchmark (QMD fixture format)",
	Long: `Run search quality benchmark against the LMD daemon.

Accepts a JSON fixture file in QMD bench format:
  {"collection":"...", "queries":[{"id":"...","query":"...","type":"...","expected_files":["..."],"expected_in_top_k":N}]}

Outputs recall@k per backend (search/vsearch/query) in table or JSON format.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fixturePath := args[0]
		data, err := os.ReadFile(fixturePath)
		if err != nil {
			return fmt.Errorf("read fixture: %w", err)
		}

		var fixture struct {
			Description string `json:"description"`
			Collection  string `json:"collection"`
			Queries     []struct {
				ID             string   `json:"id"`
				Query          string   `json:"query"`
				Type           string   `json:"type"`
				Description    string   `json:"description"`
				ExpectedFiles  []string `json:"expected_files"`
				ExpectedInTopK int      `json:"expected_in_top_k"`
			} `json:"queries"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			return fmt.Errorf("parse fixture: %w", err)
		}

		if fixture.Collection == "" {
			return fmt.Errorf("fixture must specify 'collection'")
		}

		client := daemon.NewClient(config.Cfg.Daemon.Port)

		// Results per backend per query
		type queryResult struct {
			QueryID    string   `json:"query_id"`
			QueryText  string   `json:"query"`
			Backend    string   `json:"backend"`
			RecallAtK  float64  `json:"recall_at_k"`
			HitsAtK    int      `json:"hits_at_k"`
			TotalExpected int  `json:"total_expected"`
			TopFiles   []string `json:"top_files"`
		}
		var results []queryResult

		backends := []struct {
			name     string
			endpoint string
		}{
			{"search", "/search"},
			{"vsearch", "/vsearch"},
			{"query", "/query"},
		}

		for _, q := range fixture.Queries {
			for _, be := range backends {
				k := q.ExpectedInTopK
				if k <= 0 {
					k = 5
				}
				limit := k * 3 // overfetch
				if limit < 10 {
					limit = 10
				}

				var resp struct {
					Hits []struct {
						Path string `json:"Path"`
					} `json:"hits"`
				}
				body, err := client.Post(be.endpoint, map[string]interface{}{
					"query":      q.Query,
					"collection": fixture.Collection,
					"limit":      limit,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "bench: %s/%s/%s failed: %s\n", q.ID, be.name, be.endpoint, err)
					continue
				}
				if err := json.Unmarshal(body, &resp); err != nil {
					continue
				}

				// Compute recall@k
				expected := make(map[string]bool)
				for _, f := range q.ExpectedFiles {
					expected[normalizePath(f)] = true
				}

				var topFiles []string
				hitsInTopK := 0
				for i, h := range resp.Hits {
					if i >= k {
						break
					}
					topFiles = append(topFiles, h.Path)
					if matchesAny(h.Path, expected) {
						hitsInTopK++
					}
				}

				recall := 0.0
				if len(expected) > 0 {
					recall = float64(hitsInTopK) / float64(len(expected))
				}

				results = append(results, queryResult{
					QueryID: q.ID, QueryText: q.Query, Backend: be.name,
					RecallAtK: recall, HitsAtK: hitsInTopK,
					TotalExpected: len(expected), TopFiles: topFiles,
				})
			}
		}

		if jsonOut {
			out, _ := json.MarshalIndent(map[string]interface{}{
				"fixture": filepath.Base(fixturePath),
				"results": results,
			}, "", "  ")
			fmt.Println(string(out))
			return nil
		}

		// Table output
		fmt.Printf("%-30s %-8s  Recall@k  Hits/k  Expected\n", "Query", "Backend")
		fmt.Printf("%-30s %-8s  --------  ------  --------\n", strings.Repeat("-", 30), strings.Repeat("-", 8))
		for _, r := range results {
			fmt.Printf("%-30s %-8s    %5.1f%%    %2d/%-2d    %d\n",
				truncate(r.QueryID, 30), r.Backend,
				r.RecallAtK*100, r.HitsAtK, r.TotalExpected, r.TotalExpected)
		}

		// Summary
		fmt.Println()
		fmt.Printf("Summary (avg recall@k):\n")
		sums := map[string]struct{ sum, count float64 }{}
		for _, r := range results {
			s := sums[r.Backend]
			s.sum += r.RecallAtK
			s.count++
			sums[r.Backend] = s
		}
		for _, be := range backends {
			s := sums[be.name]
			if s.count > 0 {
				fmt.Printf("  %-8s  %5.1f%%\n", be.name, s.sum/s.count*100)
			}
		}

		return nil
	},
}

func normalizePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "qmd://")
	return strings.ToLower(strings.Trim(p, "/"))
}

func matchesAny(candidate string, expected map[string]bool) bool {
	c := normalizePath(candidate)
	for e := range expected {
		if c == e {
			return true
		}
		// Suffix match: expected is a filename, candidate ends with it
		if strings.HasSuffix(c, e) || strings.HasSuffix(c, "/"+e) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func init() {
	benchCmd.Flags().StringVarP(&benchFixture, "fixture", "f", "", "fixture JSON file (alternative to positional arg)")
	rootCmd.AddCommand(benchCmd)
}

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

var benchMode string
var benchStrategy string
var benchLimit int

var benchCmd = &cobra.Command{
	Use:   "bench [longmemeval|<fixture.json>]",
	Short: "Run search quality benchmark",
	Long: `Run search quality benchmark against the LMD daemon.

Built-in benchmarks:
  lmd bench longmemeval            LongMemEval 500 questions
  lmd bench longmemeval --limit 50 First 50 questions

QMD fixture format:
  lmd bench fixture.json           Run custom fixture'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]

		if target == "longmemeval" {
			return runLongMemEval()
		}
		return runFixtureBench(target)
	},
}

// ─── Backend types ─────────────────────────────────────────────────────────

type benchBackend struct {
	name     string
	endpoint string
}

var benchBackends = []benchBackend{
	{"search", "/search"},
	{"vsearch", "/vsearch"},
	{"query", "/query"},
}

// ─── Fixture ───────────────────────────────────────────────────────────────

func runFixtureBench(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}
	var fixture struct {
		Collection string `json:"collection"`
		Queries    []struct {
			ID             string   `json:"id"`
			Query          string   `json:"query"`
			Type           string   `json:"type"`
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

	var queries []benchQuery
	for _, q := range fixture.Queries {
		queries = append(queries, benchQuery{
			ID: q.ID, Query: q.Query, Type: q.Type,
			ExpectedFiles: q.ExpectedFiles, ExpectedInTopK: q.ExpectedInTopK,
		})
	}
	return runBench(fixture.Collection, queries, benchBackends...)
}

// ─── LongMemEval built-in ──────────────────────────────────────────────────

func runLongMemEval() error {
	home, _ := os.UserHomeDir()
	cacheFile := filepath.Join(home, ".cache", "lmd", "benchmarks", "longmemeval_s_cleaned.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return fmt.Errorf("LongMemEval dataset not found at %s: %w", cacheFile, err)
	}

	var dataset []struct {
		Question         string     `json:"question"`
		QuestionType     string     `json:"question_type"`
		HaystackSessions [][]struct {
			Role      string `json:"role"`
			HasAnswer bool   `json:"has_answer"`
		} `json:"haystack_sessions"`
	}
	if err := json.Unmarshal(data, &dataset); err != nil {
		return fmt.Errorf("parse dataset: %w", err)
	}

	var queries []benchQuery

	if benchLimit > 0 && benchLimit < len(dataset) {
		dataset = dataset[:benchLimit]
	}

	for qi, q := range dataset {
		var expected []string
		for si, sess := range q.HaystackSessions {
			for _, t := range sess {
				if t.HasAnswer {
					expected = append(expected, fmt.Sprintf("q%04d_s%03d.md", qi+1, si+1))
					break
				}
			}
		}
		if len(expected) == 0 {
			continue
		}
		queries = append(queries, benchQuery{
			ID:             fmt.Sprintf("q%04d", qi+1),
			Query:          q.Question,
			Type:           q.QuestionType,
			ExpectedFiles:  expected,
			ExpectedInTopK: 10,
		})
	}

	backends := benchBackends
	if benchMode != "all" && benchMode != "" {
		backends = filterBackends(benchMode)
	}

	return runBench("bench_full", queries, backends...)
}

func filterBackends(mode string) []benchBackend {
	for _, be := range benchBackends {
		if be.name == mode {
			return []benchBackend{be}
		}
	}
	return benchBackends
}

// ─── Core bench runner ─────────────────────────────────────────────────────

type benchQuery struct {
	ID             string
	Query          string
	Type           string
	ExpectedFiles  []string
	ExpectedInTopK int
}

type hitRecord struct {
	Path string
}

type benchResult struct {
	QueryID     string   `json:"query_id"`
	QueryText   string   `json:"query"`
	Backend     string   `json:"backend"`
	R5          float64  `json:"r5"`
	R10         float64  `json:"r10"`
	MRR         float64  `json:"mrr"`
	HitsAt10    int      `json:"hits_at_10"`
	Expected    int      `json:"expected"`
}

func runBench(collection string, queries []benchQuery, backends ...benchBackend) error {
	client := daemon.NewClient(config.Cfg.Daemon.Port)

	if backends == nil {
		backends = benchBackends
	}

	var results []benchResult

	// Build expected files set per query (normalized)
	type queryExpects map[string]bool
	expectsMap := make(map[string]queryExpects)
	for _, q := range queries {
		em := make(queryExpects)
		for _, f := range q.ExpectedFiles {
			em[normalizePath(f)] = true
		}
		expectsMap[q.ID] = em
	}

	for _, q := range queries {
		em := expectsMap[q.ID]
		for _, be := range backends {
			k := q.ExpectedInTopK
			if k <= 0 {
				k = 10
			}
			limit := k * 3
			if limit < 10 {
				limit = 10
			}

			body := map[string]interface{}{
				"query":      q.Query,
				"collection": collection,
				"limit":      limit,
			}
			if benchStrategy != "" {
				body["strategy"] = benchStrategy
			}

			respBody, err := client.Post(be.endpoint, body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bench: %s/%s failed: %s\n", q.ID, be.name, err)
				continue
			}

			var resp struct {
				Hits []struct {
					Path string `json:"Path"`
				} `json:"hits"`
			}
			if err := json.Unmarshal(respBody, &resp); err != nil {
				continue
			}

			// R@5, R@10, MRR — dedup by file path within each K window
			r5 := recallAtK(resp.Hits, em, 5)
			r10 := recallAtK(resp.Hits, em, 10)
			mrr := mrrAtK(resp.Hits, em, 10)

			results = append(results, benchResult{
				QueryID: q.ID, QueryText: q.Query, Backend: be.name,
				R5: r5, R10: r10, MRR: mrr,
				HitsAt10: countHits(resp.Hits, em, 10),
				Expected: len(em),
			})
		}
	}

	printBenchOutput(results, queries, backends)
	return nil
}

func recallAtK(hits []struct{Path string `json:"Path"`}, expected map[string]bool, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	found := 0
	seen := make(map[string]bool)
	for i, h := range hits {
		if i >= k {
			break
		}
		n := normalizePath(h.Path)
		for e := range expected {
			if matchesPath(n, e) && !seen[e] {
				seen[e] = true
				found++
			}
		}
	}
	return float64(found) / float64(len(expected))
}

func mrrAtK(hits []struct{Path string `json:"Path"`}, expected map[string]bool, k int) float64 {
	seen := make(map[string]bool)
	for i, h := range hits {
		if i >= k {
			break
		}
		n := normalizePath(h.Path)
		for e := range expected {
			if matchesPath(n, e) && !seen[e] {
				return 1.0 / float64(i+1)
			}
		}
	}
	return 0
}

func countHits(hits []struct{Path string `json:"Path"`}, expected map[string]bool, k int) int {
	count := 0
	seen := make(map[string]bool)
	for i, h := range hits {
		if i >= k {
			break
		}
		n := normalizePath(h.Path)
		for e := range expected {
			if matchesPath(n, e) && !seen[e] {
				seen[e] = true
				count++
			}
		}
	}
	return count
}

func normalizePath(p string) string {
	if p == "" {
		return p
	}
	p = strings.TrimPrefix(p, "qmd://")
	p = strings.TrimPrefix(p, "./")
	return strings.Trim(strings.ToLower(p), "/")
}

func matchesPath(candidate, expected string) bool {
	c := filepath.ToSlash(candidate)
	e := filepath.ToSlash(expected)
	if c == e {
		return true
	}
	return strings.HasSuffix(c, "/"+e) || strings.HasSuffix(c, "\\"+e)
}

// ─── Output ────────────────────────────────────────────────────────────────

func printBenchOutput(results []benchResult, queries []benchQuery, backends []benchBackend) {
	if jsonOut {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"queries": len(queries),
			"results": results,
		}, "", "  ")
		fmt.Println(string(out))
		return
	}

	// Per-query detail
	fmt.Printf("%-8s %-50s %-8s  %6s  %6s  %6s  %7s\n", "Query", "Q Text", "Backend", "R@5", "R@10", "MRR", "Hits/Exp")
	fmt.Printf("%-8s %-50s %-8s  %6s  %6s  %6s  %7s\n",
		strings.Repeat("-", 8), strings.Repeat("-", 50), strings.Repeat("-", 8),
		strings.Repeat("-", 6), strings.Repeat("-", 6), strings.Repeat("-", 6), strings.Repeat("-", 7))

	for _, r := range results {
		qt := r.QueryText
		if len(qt) > 50 {
			qt = qt[:50]
		}
		fmt.Printf("%-8s %-50s %-8s  %5.1f%%  %5.1f%%  %6.3f  %2d/%-2d\n",
			r.QueryID, qt, r.Backend, r.R5*100, r.R10*100, r.MRR, r.HitsAt10, r.Expected)
	}

	// Summary
	fmt.Println()
	fmt.Printf("%-8s  %6s  %6s  %6s\n", "Summary", "R@5", "R@10", "MRR")
	fmt.Printf("%-8s  %6s  %6s  %6s\n",
		strings.Repeat("-", 8), strings.Repeat("-", 6), strings.Repeat("-", 6), strings.Repeat("-", 6))

	type sum struct{ r5, r10, mrr, n float64 }
	sums := make(map[string]sum)
	for _, r := range results {
		s := sums[r.Backend]
		s.r5 += r.R5
		s.r10 += r.R10
		s.mrr += r.MRR
		s.n++
		sums[r.Backend] = s
	}
	for _, be := range backends {
		s := sums[be.name]
		if s.n > 0 {
			fmt.Printf("%-8s  %5.1f%%  %5.1f%%  %6.3f  (%d queries)\n",
				be.name, s.r5/s.n*100, s.r10/s.n*100, s.mrr/s.n, int(s.n))
		}
	}
}

// ─── CLI init ──────────────────────────────────────────────────────────────

func init() {
	benchCmd.Flags().StringVar(&benchMode, "mode", "all", "backends: search|vsearch|query|all")
	benchCmd.Flags().StringVar(&benchStrategy, "strategy", "or", "FTS strategy: or|df")
	benchCmd.Flags().IntVar(&benchLimit, "limit", 0, "limit questions (longmemeval only)")
	rootCmd.AddCommand(benchCmd)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

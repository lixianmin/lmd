# Phase 3 - Hybrid Search Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan.

**Goal:** Add RRF hybrid fusion search (`query` command) that combines BM25 and vector results, plus output formatter system.

**Architecture:** The `query` command runs both BM25 and vector search in parallel, then fuses results using Reciprocal Rank Fusion (RRF). Output is formatted via a pluggable formatter system (text/json/md/csv). Reranker and query expansion are deferred to Phase 3.5 (GGUF model loading).

**Tech Stack:** Existing store/embedding/tokenizer layers, no new external dependencies.

**Spec:** `docs/superpowers/specs/2026-04-12-lmd-design.md`

**Depends on:** Phase 1 + Phase 2 (complete)

---

## Chunk 1: Output Formatter System

### Task 1: Formatter interface + text/json implementations

**Files:**
- Create: `internal/formatter/formatter.go`
- Create: `internal/formatter/text.go`
- Create: `internal/formatter/json.go`
- Test: `internal/formatter/formatter_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/formatter/formatter_test.go`:
```go
package formatter

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextFormatter(t *testing.T) {
	hits := []SearchHit{
		{DocID: "abc", Collection: "notes", Path: "go.md", Title: "Go并发", Score: 0.95, Snippet: "goroutine...", Line: 42},
		{DocID: "def", Collection: "notes", Path: "python.md", Title: "Python", Score: 0.80, Snippet: "pandas...", Line: 10},
	}
	f := NewTextFormatter(TextConfig{Full: false})
	var buf bytes.Buffer
	err := f.Format(&buf, hits)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "go.md") {
		t.Fatal("expected go.md in output")
	}
	if !strings.Contains(out, "95%") {
		t.Fatal("expected 95% in output")
	}
}

func TestTextFormatterEmpty(t *testing.T) {
	f := NewTextFormatter(TextConfig{})
	var buf bytes.Buffer
	err := f.Format(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No results") {
		t.Fatal("expected 'No results' for empty hits")
	}
}

func TestJSONFormatter(t *testing.T) {
	hits := []SearchHit{
		{DocID: "abc", Path: "go.md", Title: "Go", Score: 0.9, Snippet: "hello", Line: 1},
	}
	f := NewJSONFormatter()
	var buf bytes.Buffer
	err := f.Format(&buf, hits)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"path"`) {
		t.Fatal("expected JSON with path field")
	}
	if !strings.Contains(out, `"doc_id"`) {
		t.Fatal("expected JSON with doc_id field")
	}
}

func TestJSONFormatterEmpty(t *testing.T) {
	f := NewJSONFormatter()
	var buf bytes.Buffer
	err := f.Format(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]" {
		t.Fatalf("expected '[]' for empty hits, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Implement formatter interface + types**

Create `internal/formatter/formatter.go`:
```go
package formatter

import "io"

type SearchHit struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Formatter interface {
	Format(w io.Writer, hits []SearchHit) error
}
```

Create `internal/formatter/text.go`:
```go
package formatter

import (
	"fmt"
	"io"
)

type TextConfig struct {
	Full bool
}

type TextFormatter struct {
	config TextConfig
}

func NewTextFormatter(config TextConfig) *TextFormatter {
	return &TextFormatter{config: config}
}

func (f *TextFormatter) Format(w io.Writer, hits []SearchHit) error {
	if len(hits) == 0 {
		fmt.Fprintln(w, "No results found.")
		return nil
	}

	for _, r := range hits {
		fmt.Fprintf(w, "%s:%d #%s\n", r.Path, r.Line, r.DocID)
		fmt.Fprintf(w, "Title: %s\n", r.Title)
		fmt.Fprintf(w, "Score: %.0f%%\n", r.Score*100)
		if f.config.Full {
			fmt.Fprintln(w)
			fmt.Fprintln(w, r.Snippet)
		} else {
			fmt.Fprintf(w, "\n%s\n", r.Snippet)
		}
		fmt.Fprintln(w)
	}
	return nil
}
```

Create `internal/formatter/json.go`:
```go
package formatter

import (
	"encoding/json"
	"io"
)

type jsonHit struct {
	DocID      string  `json:"doc_id"`
	Collection string  `json:"collection,omitempty"`
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Score      float64 `json:"score"`
	Snippet    string  `json:"snippet"`
	Line       int     `json:"line"`
}

type JSONFormatter struct{}

func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

func (f *JSONFormatter) Format(w io.Writer, hits []SearchHit) error {
	if hits == nil {
		hits = []SearchHit{}
	}
	out := make([]jsonHit, len(hits))
	for i, h := range hits {
		out[i] = jsonHit{
			DocID:      h.DocID,
			Collection: h.Collection,
			Path:       h.Path,
			Title:      h.Title,
			Score:      h.Score,
			Snippet:    h.Snippet,
			Line:       h.Line,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/formatter/ -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add output formatter system (text + json)"
```

---

## Chunk 2: RRF Fusion Algorithm

### Task 2: RRF fusion in searcher

**Files:**
- Create: `internal/service/fusion.go`
- Test: `internal/service/fusion_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/service/fusion_test.go`:
```go
package service

import (
	"testing"
)

func TestRRFFusionBasic(t *testing.T) {
	lexHits := []SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
		{DocID: "c", Score: 0.5},
	}
	vecHits := []SearchHit{
		{DocID: "c", Score: 1.0},
		{DocID: "a", Score: 0.9},
		{DocID: "d", Score: 0.6},
	}

	result := FuseRRF(lexHits, vecHits, 60, 1.0)

	if len(result) == 0 {
		t.Fatal("expected fused results")
	}

	firstIDs := make(map[string]bool)
	for _, h := range result[:2] {
		firstIDs[h.DocID] = true
	}
	if !firstIDs["a"] || !firstIDs["c"] {
		t.Fatal("expected 'a' and 'c' to rank highest (appear in both lists)")
	}
}

func TestRRFFusionEmptyLex(t *testing.T) {
	vecHits := []SearchHit{
		{DocID: "a", Score: 1.0},
	}
	result := FuseRRF(nil, vecHits, 60, 1.0)
	if len(result) != 1 || result[0].DocID != "a" {
		t.Fatal("expected vector-only results when lex is empty")
	}
}

func TestRRFFusionEmptyVec(t *testing.T) {
	lexHits := []SearchHit{
		{DocID: "b", Score: 1.0},
	}
	result := FuseRRF(lexHits, nil, 60, 1.0)
	if len(result) != 1 || result[0].DocID != "b" {
		t.Fatal("expected lex-only results when vec is empty")
	}
}

func TestRRFFusionBothEmpty(t *testing.T) {
	result := FuseRRF(nil, nil, 60, 1.0)
	if len(result) != 0 {
		t.Fatal("expected empty result for empty inputs")
	}
}

func TestRRFFusionDeduplication(t *testing.T) {
	lexHits := []SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.5},
	}
	vecHits := []SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
	}
	result := FuseRRF(lexHits, vecHits, 60, 1.0)
	seen := map[string]int{}
	for _, h := range result {
		seen[h.DocID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("doc %s appeared %d times (should be deduplicated)", id, count)
		}
	}
}

func TestRRFScoreOrdering(t *testing.T) {
	lexHits := []SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
		{DocID: "e", Score: 0.3},
	}
	vecHits := []SearchHit{
		{DocID: "c", Score: 1.0},
		{DocID: "a", Score: 0.9},
		{DocID: "d", Score: 0.7},
	}
	result := FuseRRF(lexHits, vecHits, 60, 1.0)
	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Fatalf("results not sorted by score: [%d]=%.4f > [%d]=%.4f",
				i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}
```

- [ ] **Step 2: Implement RRF fusion**

Create `internal/service/fusion.go`:
```go
package service

import "sort"

func FuseRRF(lexHits, vecHits []SearchHit, k int, origWeight float64) []SearchHit {
	type scored struct {
		hit         SearchHit
		rrfScore    float64
		firstRank   int
	}

	docs := make(map[string]*scored)
	rank := 0

	for _, h := range lexHits {
		rank++
		if _, exists := docs[h.DocID]; !exists {
			docs[h.DocID] = &scored{
				hit:       h,
				firstRank: rank,
			}
		}
		docs[h.DocID].rrfScore += origWeight / float64(k+rank)
	}

	rank = 0
	for _, h := range vecHits {
		rank++
		if existing, exists := docs[h.DocID]; exists {
			existing.rrfScore += origWeight / float64(k+rank)
			if h.Snippet != "" && existing.hit.Snippet == "" {
				existing.hit.Snippet = h.Snippet
			}
		} else {
			docs[h.DocID] = &scored{
				hit:       h,
				firstRank: rank,
			}
			docs[h.DocID].rrfScore += origWeight / float64(k+rank)
		}
	}

	results := make([]SearchHit, 0, len(docs))
	for _, s := range docs {
		s.hit.Score = s.rrfScore
		results = append(results, s.hit)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > 0 {
		topScore := results[0].Score
		for i := range results {
			results[i].Score = results[i].Score / topScore
		}
	}

	return results
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/service/ -run TestRRF -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add RRF fusion algorithm for hybrid search"
```

---

## Chunk 3: Hybrid Search Service + query CLI

### Task 3: SearchHybrid method on Searcher

**Files:**
- Modify: `internal/service/searcher.go`
- Test: `internal/service/searcher_test.go` (add hybrid test)

- [ ] **Step 1: Add hybrid search test**

Add to `internal/service/searcher_test.go` (or create if needed):
```go
func TestSearchHybrid(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)
	_, _ = embedder.EmbedAll("mock", false)

	searcher := NewSearcher(db, tok)
	results, err := searcher.SearchHybrid(provider, "并发编程", "", 5, 0)
	if err != nil {
		t.Fatalf("SearchHybrid failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
}

func TestSearchHybridCollection(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)
	_, _ = embedder.EmbedAll("mock", false)

	searcher := NewSearcher(db, tok)
	results, err := searcher.SearchHybrid(provider, "并发编程", "test", 5, 0)
	if err != nil {
		t.Fatalf("SearchHybrid failed: %v", err)
	}
	for _, r := range results {
		if r.Collection != "test" {
			t.Fatalf("expected only 'test' collection, got %s", r.Collection)
		}
	}
}
```

- [ ] **Step 2: Implement SearchHybrid**

Add to `internal/service/searcher.go`:
```go
func (s *Searcher) SearchHybrid(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]SearchHit, error) {
	lexHits, err := s.SearchLex(query, collection, limit*3, 0)
	if err != nil {
		return nil, err
	}

	vecHits, err := s.SearchVector(provider, query, collection, limit*3, 0)
	if err != nil {
		return nil, err
	}

	fused := FuseRRF(lexHits, vecHits, 60, 1.0)

	var results []SearchHit
	for _, h := range fused {
		if h.Score < minScore {
			continue
		}
		results = append(results, h)
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test -tags "fts5" ./internal/service/ -run TestSearchHybrid -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add hybrid search (BM25 + vector RRF fusion)"
```

### Task 4: query CLI command + refactor search output to use formatter

**Files:**
- Modify: `internal/cli/search.go` (refactor to use formatter)
- Modify: `internal/cli/search.go` (add query command)
- Create: `internal/cli/output.go` (shared output flags/formatting helpers)

- [ ] **Step 1: Create output.go with shared formatting logic**

Create `internal/cli/output.go`:
```go
package cli

import (
	"os"

	"github.com/lixianmin/lmd/internal/formatter"
)

var (
	outputFormat string
)

func registerOutputFlags(cmd interface{ Flags() interface{ StringVar(p *string, name, value, usage string) } }) {
}

type serviceHits []struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}
```

Wait — the `SearchHit` types in `service` and `formatter` are different structs. We need to convert. Let me reconsider.

Actually, we should either:
1. Make `formatter.SearchHit` the canonical type and have `service` use it
2. Or have a conversion function

The cleaner approach: have `service.SearchHit` be the canonical type, and import it in `formatter`. But `formatter` should not depend on `service` (dependency direction). So: make `formatter.SearchHit` the canonical type, or keep them separate with conversion.

Simplest: **reuse formatter.SearchHit in service**. The formatter package defines the hit type, service populates it.

Revised approach:
- `formatter.SearchHit` is the canonical struct
- `service.Searcher` methods return `[]formatter.SearchHit`
- CLI passes hits directly to formatter

This requires changing `service/searcher.go` to return `formatter.SearchHit`. But that creates a circular import risk... No — `service` imports `formatter` is fine (formatter has no deps).

Let me revise the plan:

- [ ] **Step 1: Update formatter.SearchHit to be the canonical type, remove service.SearchHit**

Modify `internal/formatter/formatter.go` to add fields matching service needs:
```go
type SearchHit struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}
```
(This is already what we have. Good.)

- [ ] **Step 2: Update service/searcher.go to use formatter.SearchHit**

Change `service.SearchHit` to `formatter.SearchHit` everywhere in searcher.go.

In `internal/service/searcher.go`, change:
```go
import (
	"database/sql"
	"strings"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

// Remove SearchHit struct definition — use formatter.SearchHit instead
```

Update all method signatures to return `[]formatter.SearchHit`.

- [ ] **Step 3: Update all references**

Files that reference `service.SearchHit`:
- `internal/service/fusion.go` — update to use `formatter.SearchHit`
- `internal/cli/search.go` — update to use `formatter.SearchHit`
- `internal/service/embedder_test.go` — no change needed (doesn't use SearchHit)
- `pkg/lmd.go` — update SearchResult conversion

- [ ] **Step 4: Add output format flag + query command**

Add to `internal/cli/search.go`:
```go
var outputFormat string

// Add to init():
// searchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, json")
// vsearchCmd.Flags().StringVar(&outputFormat, "format", "text", "output format: text, json")
```

Add query command:
```go
var queryCmd = &cobra.Command{
	Use:   "query <query>",
	Short: "Hybrid search (BM25 + vector with RRF fusion)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		provider := embedding.NewMockProvider(1024)
		searcher := service.NewSearcher(db, tok)
		results, err := searcher.SearchHybrid(provider, args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		return formatResults(os.Stdout, results)
	},
}
```

Helper:
```go
func formatResults(w io.Writer, hits []formatter.SearchHit) error {
	switch outputFormat {
	case "json":
		return formatter.NewJSONFormatter().Format(w, hits)
	default:
		return formatter.NewTextFormatter(formatter.TextConfig{Full: searchFull}).Format(w, hits)
	}
}
```

Register query command in init():
```go
rootCmd.AddCommand(queryCmd)
```

- [ ] **Step 5: Update pkg/lmd.go**

Update `SearchResult` to convert from `formatter.SearchHit`.

- [ ] **Step 6: Build and run all tests**

Run: `make test`

- [ ] **Step 7: E2E test**

```bash
make build
rm -rf /tmp/lmd-phase3 && mkdir -p /tmp/lmd-phase3/docs
cat > /tmp/lmd-phase3/docs/go.md << 'EOF'
# Go并发编程

Go语言通过goroutine和channel实现并发编程。
goroutine是轻量级线程，channel用于goroutine间通信。
EOF
cat > /tmp/lmd-phase3/docs/python.md << 'EOF'
# Python数据科学

Python是数据科学领域最流行的语言。
pandas和numpy是核心数据处理库。
EOF

./lmd --index /tmp/lmd-phase3/test.sqlite collection add /tmp/lmd-phase3/docs --name docs
./lmd --index /tmp/lmd-phase3/test.sqlite update
./lmd --index /tmp/lmd-phase3/test.sqlite embed
echo "=== query (text) ==="
./lmd --index /tmp/lmd-phase3/test.sqlite query "并发编程"
echo "=== query (json) ==="
./lmd --index /tmp/lmd-phase3/test.sqlite query "并发编程" --format json
```

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: add query command (hybrid RRF search) + output formatters"
```

---

## Summary

Phase 3 adds:
- Output formatter system (`internal/formatter/`) with text and JSON formats
- RRF fusion algorithm (`internal/service/fusion.go`)
- Hybrid search method `SearchHybrid` on Searcher
- `query` CLI command combining BM25 + vector with RRF fusion
- `--format json` flag on all search commands
- Unified `formatter.SearchHit` as the canonical hit type

Deferred to Phase 3.5 (requires GGUF model loading):
- Reranker (Qwen3-Reranker-0.6B)
- Query expansion (Qwen2.5-1.5B-Instruct)
- MMR diversity re-ranking

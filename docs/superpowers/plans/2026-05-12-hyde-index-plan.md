# HyDE Index Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace summary-based two-level search with HyDE-based two-level search: Level 1 searches `@hyde` collection (LLM-generated hypothetical queries + algorithm-extracted keywords), Level 2 refines within matched source documents, with fallback to global hybrid search.

**Architecture:** HyDEIndexer generates HyDE data per document (LLM questions + keyword extraction), stored in `@hyde` collection reusing existing chunk infrastructure. Search flows: Level 1 hybrid search on `@hyde` → extract source_doc_ids → Level 2 hybrid search within matched docs → fallback to global hybrid search.

**Tech Stack:** Go, SQLite + FTS5 + sqlite-vec, Ollama (qwen3-embedding:0.6b), SiliconFlow (Qwen2.5-7B-Instruct)

---

### Task 1: Rename Indexer → ChunkIndexer

**Files:**
- Modify: `internal/service/indexer.go:53-65,99,285,479,551`
- Modify: `internal/daemon/daemon.go:44,77`
- Modify: `internal/daemon/daemon_routes.go` (references to `my.indexer`)
- Modify: `internal/service/indexer_test.go` (all test files)

- [ ] **Step 1: Rename struct and constructor in indexer.go**

```go
type ChunkIndexer struct {
	tokenizer       tokenizer.Tokenizer
	markdownChunker chunker.Chunker
	plainChunker    chunker.Chunker
}

func NewChunkIndexer(tok tokenizer.Tokenizer) *ChunkIndexer {
	return &ChunkIndexer{
		tokenizer:       tok,
		markdownChunker: chunker.NewMarkdownChunker(defaultChunkSize),
		plainChunker:    chunker.NewPlainTextChunker(defaultChunkSize),
	}
}
```

Replace all `*Indexer` receiver types with `*ChunkIndexer`, and all `NewIndexer` calls with `NewChunkIndexer`.

- [ ] **Step 2: Update daemon.go field and initialization**

Replace:
```go
indexer       *service.Indexer
```
with:
```go
chunkIndexer  *service.ChunkIndexer
```

Replace:
```go
my.indexer = service.NewIndexer(tok)
```
with:
```go
my.chunkIndexer = service.NewChunkIndexer(tok)
```

Replace all `my.indexer` references in daemon.go with `my.chunkIndexer`.

- [ ] **Step 3: Update daemon_routes.go** (all `my.indexer` → `my.chunkIndexer`)

- [ ] **Step 4: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go build -tags "fts5" -ldflags "-s -w" -mod=mod -o lmd github.com/lixianmin/lmd/cmd/lmd
go test -tags fts5 ./internal/service/ -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/indexer.go internal/daemon/daemon.go internal/daemon/daemon_routes.go internal/service/indexer_test.go
git commit -m "refactor: rename Indexer to ChunkIndexer"
```

---

### Task 2: Rename query → hybrid (Config + Routes + Handlers)

**Files:**
- Modify: `internal/config/config.go:22,80-86` (if `Summary` field renamed later in Task 3)
- Modify: `internal/daemon/server.go:18`
- Modify: `internal/daemon/daemon_routes.go:126-166`
- Modify: `internal/daemon/client.go:117-123`

This task is mechanical renaming: `Query` → `Hybrid`, `/query` → `/hybrid`, `handleQuery` → `handleHybrid` (note: `handleHyde` already exists and is different).

- [ ] **Step 1: Rename route in server.go**

```go
{"POST", "/hybrid", (*Daemon).handleHybrid},
```

- [ ] **Step 2: Rename handler in daemon_routes.go**

Rename `handleQuery` → `handleHybrid`, update log message:
```go
func (my *Daemon) handleHybrid(w http.ResponseWriter, r *http.Request) {
    ...
    logo.Info("handleHybrid: query=%q collection=%s lex=%d vec=%d results=%d",
        req.Query, req.Collection, len(lexHits), len(vecHits), len(results))
    ...
}
```

- [ ] **Step 3: Rename Client method**

```go
func (my *Client) Hybrid(query, collection string, limit int, minScore float64) ([]byte, error) {
	return my.Post("/hybrid", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}
```

- [ ] **Step 4: Run build + tests**

```bash
cd /Users/xmli/me/code/lmd && make build && go test -tags fts5 ./internal/... -count=1 2>&1 | tail -15
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor: rename query/handleQuery/Query to hybrid/handleHybrid/Hybrid"
```

---

### Task 3: Rename query → hybrid (CLI)

**Files:**
- Modify: `internal/cli/search.go:72-90,197,202`

- [ ] **Step 1: Rename CLI command**

```go
var hybridCmd = &cobra.Command{
	Use:   "hybrid <query>",
	Short: "Hybrid search (BM25 + vector)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.Hybrid(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}
		if jsonOut {
			printBody(body)
			return nil
		}
		return formatResponse(body)
	},
}
```

- [ ] **Step 2: Update flags and registration**

Replace `queryCmd.Flags().AddFlagSet(...)` with `hybridCmd.Flags().AddFlagSet(...)`
Replace `rootCmd.AddCommand(queryCmd)` with `rootCmd.AddCommand(hybridCmd)`

- [ ] **Step 3: Build and verify help text**

```bash
cd /Users/xmli/me/code/lmd && make build && ./lmd help | grep hybrid
```

Expected: shows `hybrid` command

- [ ] **Step 4: Commit**

```bash
git add internal/cli/search.go && git commit -m "refactor: rename lmd query to lmd hybrid"
```

---

### Task 4: Rename query → hybrid (MCP + Bench)

**Files:**
- Modify: `internal/daemon/daemon_mcp.go` (MCP tool `"query"` → `"hybrid"`)
- Modify: `internal/cli/bench.go:52` (backend name and endpoint)

- [ ] **Step 1: Update MCP tool name**

Find `case "query":` in daemon_mcp.go and change to `case "hybrid":`

- [ ] **Step 2: Update bench backends**

```go
var benchBackends = []benchBackend{
	{"search", "/search", true},
	{"vsearch", "/vsearch", true},
	{"hybrid", "/hybrid", true},
}
```

Remove `{"smart-query", "/smart-query", false}` (will be deleted separately in Task 7).

- [ ] **Step 3: Build**

```bash
cd /Users/xmli/me/code/lmd && make build
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon_mcp.go internal/cli/bench.go && git commit -m "refactor: rename query → hybrid in MCP and bench"
```

---

### Task 5: Config: SummaryConfig → HydeConfig

**Files:**
- Modify: `internal/config/config.go:48-54,80-86,314-322`
- Modify: `internal/config/config_test.go`
- Modify: `internal/daemon/daemon.go:143` and `newLLM`
- Modify: `~/.config/lmd/config.yaml`
- Modify: `internal/service/processor.go:25`

- [ ] **Step 1: Rename struct and field in config.go**

Replace `SummaryConfig` with `HydeConfig`:

```go
type HydeConfig struct {
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	MaxInputTokens  int    `yaml:"max_input_tokens"`
	NoThinking      bool   `yaml:"no_thinking"`
}
```

Replace `Summary HydeConfig` in Config struct with `Hyde HydeConfig \`yaml:"hyde"\``.

- [ ] **Step 2: Update DefaultConfig**

```go
Hyde: HydeConfig{
	Provider:        "siliconflow",
	Model:           "Qwen/Qwen2.5-7B-Instruct",
	MaxOutputTokens: 512,
	MaxInputTokens:  30000,
	NoThinking:      true,
},
```

- [ ] **Step 3: Update newLLM in daemon.go**

```go
func newLLM(config *config.Config) llm.LLMProvider {
	var llmProv llm.LLMProvider
	switch config.Hyde.Provider {
	case "ollama":
		llmProv = llm.NewOllamaLLM(config.Providers.Ollama.BaseURL, config.Hyde.Model, config.Hyde.NoThinking)
	case "siliconflow":
		llmProv = llm.NewSiliconFlowLLM(config.Providers.SiliconFlow.BaseURL, config.Hyde.Model, config.Providers.SiliconFlow.APIKey)
	case "deepseek":
		llmProv = llm.NewSiliconFlowLLM(config.Providers.DeepSeek.BaseURL, config.Hyde.Model, config.Providers.DeepSeek.APIKey)
	default:
		logo.Error("unknown hyde provider: %s", config.Hyde.Provider)
		return nil
	}
	return llmProv
}
```

- [ ] **Step 4: Update daemon.go goLoop reference**

Change `my.cfg.Summary` to `my.cfg.Hyde`:
```go
processor := service.NewProcessor(my.embedProvider, my.llmProvider, my.cfg.Hyde)
```

- [ ] **Step 5: Update config_test.go**

Replace all `cfg.Summary` with `cfg.Hyde` and `SummaryConfig` with `HydeConfig`. Update expected values.

- [ ] **Step 6: Update config.yaml**

```yaml
hyde:
    provider: siliconflow
    model: Qwen/Qwen2.5-7B-Instruct
    max_output_tokens: 512
    max_input_tokens: 30000
    no_thinking: true
```

- [ ] **Step 7: Update NewProcessor signature**

```go
func NewProcessor(embedProv embedding.EmbeddingProvider, llmProv llm.LLMProvider, cfg config.HydeConfig) *Processor {
```

- [ ] **Step 8: Build + test**

```bash
cd /Users/xmli/me/code/lmd && make build && go test -tags fts5 ./internal/... -count=1 2>&1 | tail -15
```

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "refactor: SummaryConfig → HydeConfig, config.Summary → config.Hyde"
```

---

### Task 6: Remove summary generation from Processor

**Files:**
- Modify: `internal/service/processor.go` — lines 98-161 (summary logic removal)
- Modify: `internal/service/processor_test.go` — remove summary-related tests
- Modify: `internal/dao/document.go` — keep `UpsertSummaryWithVector` (will rename in Task 8)

- [ ] **Step 1: Remove summary generation from processDocNew**

Remove lines 98-114 (summary generation + embedding + storage) from `processDocNew`. The `NewProcessor` still takes `config.HydeConfig` but doesn't use it yet — just keep it in the struct.

Updated `processDocNew`:
```go
func (my *Processor) processDocNew(ctx context.Context, doc PendingDoc) error {
	totalStart := time.Now()

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.Hash)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	var embedDuration time.Duration
	var insertDuration time.Duration
	batchSize := 8
	for i := 0; i < len(doc.Chunks); i += batchSize {
		end := min(i+batchSize, len(doc.Chunks))
		batch := doc.Chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		t := time.Now()
		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		embedDuration += time.Since(t)
		if err != nil {
			return fmt.Errorf("embed chunks batch %d: %w", i/batchSize, err)
		}

		tokenized := make([]string, len(batch))
		for j, c := range batch {
			tokenized[j] = c.Content
		}

		t = time.Now()
		if _, err := dao.InsertChunksAndVectors(docId, doc.Collection, i, batch, tokenized, vecs); err != nil {
			return fmt.Errorf("insert chunks batch %d: %w", i/batchSize, err)
		}
		insertDuration += time.Since(t)
	}

	if err := dao.CompleteDocument(docId, doc.FileModTime); err != nil {
		return fmt.Errorf("complete document: %w", err)
	}

	fullPath := doc.RootDir + "/" + doc.Path
	total := time.Since(totalStart)
	logo.Info("processor: doc %d (%s) chunks=%d embed=%.2fs insert=%.2fs total=%.2fs",
		docId, fullPath,
		len(doc.Chunks), embedDuration.Seconds(), insertDuration.Seconds(),
		total.Seconds())

	return nil
}
```

- [ ] **Step 2: Remove generateSummary, truncateContent, isDegenerate, truncateString functions**

Delete lines 131-212 (entire generateSummary, isDegenerate, truncateContent functions).

- [ ] **Step 3: Remove summary-related imports**

Remove unused imports: `bytes`, `compress/flate`, `unicode/utf8` (used only by isDegenerate and truncateContent).

- [ ] **Step 4: Update processor_test.go**

Remove tests for `generateSummary`, `isDegenerate`. Remove summary-related assertions.

- [ ] **Step 5: Build + test**

```bash
cd /Users/xmli/me/code/lmd && make build && go test -tags fts5 ./internal/... -count=1 2>&1 | tail -15
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: remove summary generation from Processor"
```

---

### Task 7: Delete smart-query

**Files:**
- Delete: smart-query route in `internal/daemon/server.go`
- Delete: `handleSmartQuery` in `internal/daemon/daemon_routes.go:195-297`
- Delete: `SmartQuery` in `internal/daemon/client.go:126-132`
- Delete: `smartQueryCmd` in `internal/cli/search.go:132-162,204`

- [ ] **Step 1: Remove from server.go**

Remove line: `{"POST", "/smart-query", (*Daemon).handleSmartQuery},`

- [ ] **Step 2: Remove from daemon_routes.go**

Delete `handleSmartQuery` function (lines 195-297).

- [ ] **Step 3: Remove from client.go**

Delete `SmartQuery` method (lines 126-132).

- [ ] **Step 4: Remove from CLI**

Delete `smartQueryCmd` (lines 132-162) and `rootCmd.AddCommand(smartQueryCmd)` (line 204).

- [ ] **Step 5: Remove from bench.go**

Remove `{"smart-query", "/smart-query", false}` from benchBackends (already done in Task 4).

- [ ] **Step 6: Build**

```bash
cd /Users/xmli/me/code/lmd && make build
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: remove smart-query (replaced by hyde)"
```

---

### Task 8: Create HyDEIndexer

**Files:**
- Create: `internal/service/hyde_indexer.go`
- Create: `internal/service/hyde_indexer_test.go`

This is the core new component. Each document generates:
1. LLM: 5-10 hypothetical questions
2. Algorithm: high-surprise keywords from original text
3. Stored as one chunk in `@hyde` collection

- [ ] **Step 1: Write failing test**

Create `internal/service/hyde_indexer_test.go`:

```go
package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
)

func TestExtractKeywords(t *testing.T) {
	text := "I have a cat named Whiskers. Whiskers is a gray tabby. I feed Whiskers Fancy Feast brand food. My bedroom walls are light gray."
	kw := extractKeywords(text)
	
	// Must contain high-frequency non-stopword terms
	hasWhiskers := false
	hasGray := false
	hasBedroom := false
	for _, k := range kw {
		if k == "Whiskers" { hasWhiskers = true }
		if k == "gray" { hasGray = true }
		if k == "bedroom" { hasBedroom = true }
	}
	if !hasWhiskers || !hasGray || !hasBedroom {
		t.Fatalf("missing keywords: whiskers=%v gray=%v bedroom=%v, got: %v", hasWhiskers, hasGray, hasBedroom, kw)
	}
}

func TestGenerateQuestions(t *testing.T) {
	mockLLM := llm.NewMockLLM("What color is the bedroom?")
	h := &HyDEIndexer{
		llm:       mockLLM,
		embedProv: embedding.NewMockProvider(1024),
	}
	
	questions, err := h.generateQuestions(t.Context(), "The bedroom walls are painted light gray. The ceiling is white.")
	if err != nil {
		t.Fatalf("generateQuestions: %v", err)
	}
	if len(questions) == 0 {
		t.Fatal("expected at least 1 question")
	}
}
```

Run: `go test -tags fts5 ./internal/service/ -run TestExtractKeywords -count=1`
Expected: FAIL (function not defined)

- [ ] **Step 2: Write minimal implementation**

Create `internal/service/hyde_indexer.go`:

```go
package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

type HyDEIndexer struct {
	llm       llm.LLMProvider
	embedProv embedding.EmbeddingProvider
	tokenizer tokenizer.Tokenizer
	maxOutput int
	maxInput  int
}

func NewHyDEIndexer(llmProv llm.LLMProvider, embedProv embedding.EmbeddingProvider, tok tokenizer.Tokenizer, cfg config.HydeConfig) *HyDEIndexer {
	return &HyDEIndexer{
		llm:       llmProv,
		embedProv: embedProv,
		tokenizer: tok,
		maxOutput: cfg.MaxOutputTokens,
		maxInput:  cfg.MaxInputTokens,
	}
}

func (my *HyDEIndexer) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	if doc.Action == DocDeleted {
		return nil
	}

	// 1. Generate hypothetical questions via LLM
	questions, err := my.generateQuestions(ctx, doc.Body)
	if err != nil {
		return fmt.Errorf("generate questions: %w", err)
	}

	// 2. Extract high-surprise keywords from original text
	keywords := extractKeywords(doc.Body)

	// 3. Concatenate into one chunk
	content := "QUESTIONS:\n" + strings.Join(questions, "\n") + "\n\nKEYWORDS:\n" + strings.Join(keywords, ", ")

	// 4. Embed the concatenated text
	vecs, err := my.embedProv.EmbedBatch(ctx, []string{content})
	if err != nil {
		return fmt.Errorf("embed hyde data: %w", err)
	}

	// 5. Get the source document (need its ID for UpsertHydeData)
	sourceDoc, err := dao.GetDocumentByPath(doc.Collection, doc.Path)
	if err != nil || sourceDoc == nil {
		return fmt.Errorf("find source doc %s/%s: %w", doc.Collection, doc.Path, err)
	}

	// 6. Store in @hyde collection
	docHash := doc.Hash
	if _, err := dao.UpsertHydeData(sourceDoc.Id, docHash, content, content, vecs[0]); err != nil {
		return fmt.Errorf("insert hyde data: %w", err)
	}

	logo.JsonI("hyde_doc", sourceDoc.Id, "path", doc.Path, "questions", len(questions), "keywords", len(keywords))
	return nil
}

func (my *HyDEIndexer) generateQuestions(ctx context.Context, content string) ([]string, error) {
	prompt := "Given the document below, generate 5-10 questions that this document could answer. " +
		"Focus on specific facts, details, and information mentioned in the text. " +
		"One question per line. Be specific — ask about names, numbers, places, dates, colors, preferences.\n\n" +
		"Document:\n" + content[:min(8000, len(content))] + "\n\nQuestions:"

	messages := []llm.Message{{Role: "user", Content: prompt}}

	t0 := time.Now()
	response, err := my.llm.ChatCompletion(ctx, messages)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(response), "\n")
	var questions []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Filter lines that look like questions (contain "?" or start with question words)
		if len(line) > 10 && (strings.HasSuffix(line, "?") || strings.Contains(line, "?")) {
			questions = append(questions, line)
		}
	}

	if len(questions) == 0 {
		questions = append(questions, response)
	}

	logo.Info("hydeIndexer: generated %d questions in %s", len(questions), time.Since(t0))
	return questions, nil
}

func extractKeywords(content string) []string {
	// Use existing tokenizer
	// ... (see expanded implementation below)
}

func (my *HyDEIndexer) ScanChanges(collectionName, rootDir, globPattern string, ignorePatterns []string) ([]PendingDoc, error) {
	// Reuse ChunkIndexer's ScanChanges to detect file changes
	ci := &ChunkIndexer{tokenizer: my.tokenizer}
	return ci.ScanChanges(collectionName, rootDir, globPattern, ignorePatterns)
}
```

- [ ] **Step 3: Implement extractKeywords with existing tokenizer**

```go
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "can": true, "shall": true,
	"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
	"at": true, "by": true, "from": true, "as": true, "into": true, "about": true,
	"i": true, "me": true, "my": true, "we": true, "our": true, "you": true, "your": true,
	"he": true, "she": true, "it": true, "they": true, "them": true, "this": true,
	"that": true, "these": true, "those": true, "and": true, "or": true, "not": true,
	"but": true, "so": true, "if": true, "then": true, "than": true, "also": true,
	"very": true, "just": true, "some": true, "more": true, "what": true, "when": true,
	"where": true, "which": true, "who": true, "how": true,
}

func extractKeywords(content string) []string {
	type freq struct {
		word  string
		count int
	}

	freqs := make(map[string]int)
	words := strings.Fields(content)
	for _, w := range words {
		w = strings.Trim(w, ".,?!;:()[]{}'\"-")
		if len(w) <= 2 {
			continue
		}
		if stopWords[strings.ToLower(w)] {
			continue
		}
		freqs[w]++
	}

	list := make([]freq, 0, len(freqs))
	for w, c := range freqs {
		list = append(list, freq{w, c})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].count > list[j].count })

	// Take top-30, but also prefer proper nouns (capitalized) and longer words
	top := 30
	if len(list) < top {
		top = len(list)
	}
	result := make([]string, 0, top)
	for i := 0; i < top && len(result) < top; i++ {
		result = append(result, list[i].word)
	}

	return result
}
```

- [ ] **Step 4: Run test to verify**

```bash
go test -tags fts5 ./internal/service/ -run TestExtractKeywords -count=1
```

Expected: PASS

- [ ] **Step 5: Add UpsertHydeData in dao/document.go** (renamed from UpsertSummaryWithVector)

Rename `UpsertSummaryWithVector` → `UpsertHydeData`, update SQL to use `@hyde` collection:
```go
func UpsertHydeData(sourceDocId int64, hash, content, tokenizedContent string, vec []float32) (int64, error) {
    // Same logic as UpsertSummaryWithVector but collection = "@hyde"
}
```

Change `@summaries` to `@hyde` in the SQL queries.

- [ ] **Step 6: Commit**

```bash
git add internal/service/hyde_indexer.go internal/service/hyde_indexer_test.go internal/dao/document.go
git commit -m "feat: add HyDEIndexer with question generation and keyword extraction"
```

---

### Task 9: Integrate HyDEIndexer into Daemon

**Files:**
- Modify: `internal/daemon/daemon.go` — add hydeIndexer field and hyde pipeline loop

- [ ] **Step 1: Add hydeIndexer to Daemon struct**

```go
type Daemon struct {
	...
	chunkIndexer  *service.ChunkIndexer
	hydeIndexer   *service.HyDEIndexer
	...
}
```

- [ ] **Step 2: Initialize hydeIndexer in Start**

```go
my.hydeIndexer = service.NewHyDEIndexer(my.llmProvider, my.embedProvider, tok, my.cfg.Hyde)
```

- [ ] **Step 3: Create hyde pipeline loop in goLoop**

Add a second ticker and goroutine for HyDE processing:

```go
func (my *Daemon) goLoop(later loom.Later) {
	processor := service.NewProcessor(my.embedProvider, my.llmProvider, my.cfg.Hyde)
	closeChan := my.wc.C()

	const indexSyncInterval = 60 * time.Second
	pipelineTicker := later.NewTicker(indexSyncInterval)
	hydeTicker := later.NewTicker(indexSyncInterval)

	my.runPipeline(processor, closeChan)
	my.runHydePipeline(closeChan)

	for {
		select {
		case <-closeChan:
			return
		case <-pipelineTicker.C:
			my.runPipeline(processor, closeChan)
		case <-hydeTicker.C:
			my.runHydePipeline(closeChan)
		}
	}
}

func (my *Daemon) runHydePipeline(closeChan <-chan struct{}) {
	cols, err := dao.ListCollections()
	if err != nil {
		return
	}
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue
		}
		pending, err := my.hydeIndexer.ScanChanges(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Warn("hydePipeline: scan %s failed: %s", col.Name, err)
			continue
		}
		for _, doc := range pending {
			select {
			case <-closeChan:
				return
			default:
			}
			if err := my.hydeIndexer.ProcessDoc(context.Background(), doc); err != nil {
				logo.Warn("hydePipeline: process %s/%s failed: %s", doc.Collection, doc.Path, err)
			}
		}
	}
}
```

- [ ] **Step 4: Build and verify daemon starts**

```bash
cd /Users/xmli/me/code/lmd && make build
./lmd stop 2>/dev/null; nohup ./lmd daemon-start > /tmp/lmd.log 2>&1 &
sleep 5 && ps aux | grep "lmd daemon" | grep -v grep
```

Expected: daemon running

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/daemon.go && git commit -m "feat: integrate HyDEIndexer into daemon pipeline"
```

---

### Task 10: Transform handleHyde to two-level search

**Files:**
- Modify: `internal/daemon/daemon_routes.go` — replace handleHyde (placeholder) with two-level search

- [ ] **Step 1: Write new handleHyde**

Replace the placeholder handleHyde (lines 168-193) with two-level search logic:

```go
func (my *Daemon) handleHyde(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
		Strategy   string  `json:"strategy"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	if req.Strategy == "" {
		req.Strategy = "pos-or"
	}

	ctx := r.Context()
	overfetch := safeOverfetch(req.Limit)

	// Level 1: search @hyde collection
	lexHits, lexErr := my.searcher.SearchLex(req.Query, "@hyde", overfetch, 0, req.Strategy)
	vecHits, vecErr := my.searcher.SearchVector(ctx, my.embedProvider, req.Query, "@hyde", overfetch, 0)
	if lexErr != nil && vecErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hyde search failed"})
		return
	}
	hydeHits := service.FuseResults(lexHits, vecHits)

	// Fallback: no hyde matches → global hybrid search
	if len(hydeHits) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	// Resolve hyde doc_ids → source_doc_ids
	uniqueRowIds := make(map[int64]bool)
	for _, h := range hydeHits {
		if h.DocRowId > 0 {
			uniqueRowIds[h.DocRowId] = true
		}
	}
	docIdSet := make(map[int64]bool)
	if len(uniqueRowIds) > 0 {
		ids := make([]int64, 0, len(uniqueRowIds))
		for id := range uniqueRowIds {
			ids = append(ids, id)
		}
		docs, err := dao.GetDocumentsByIds(ids)
		if err == nil {
			for _, doc := range docs {
				if doc.SourceDocId > 0 {
					docIdSet[doc.SourceDocId] = true
				}
			}
		}
	}

	if len(docIdSet) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	// Level 2: hybrid search within matched source docs
	sourceDocIds := make([]int64, 0, len(docIdSet))
	for id := range docIdSet {
		sourceDocIds = append(sourceDocIds, id)
	}

	lexHits2, _ := my.searcher.SearchLexByDocIds(req.Query, sourceDocIds, overfetch*2, req.Strategy)
	vecHits2, _ := my.searcher.SearchVectorByDocIds(ctx, my.embedProvider, req.Query, sourceDocIds, overfetch*2)

	results := service.FuseResults(lexHits2, vecHits2)
	results = filterAndLimit(results, req.MinScore, req.Limit)

	logo.Info("handleHyde: query=%q hyde=%d docs=%d results=%d routed=true",
		req.Query, len(hydeHits), len(docIdSet), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": true})
}
```

- [ ] **Step 2: Keep fullHybridSearch unchanged** (it already exists at line 299-305)

- [ ] **Step 3: Build**

```bash
cd /Users/xmli/me/code/lmd && make build
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon_routes.go && git commit -m "feat: replace handleHyde with two-level search (Level1 @hyde → Level2 source docs → fallback)"
```

---

### Task 11: Update CLI hyde command

**Files:**
- Modify: `internal/cli/search.go` — hydeCmd, Client.HyDE

- [ ] **Step 1: Simplify CLI hyde command**

Replace hydeCmd with simple response format (no more HyDE document generation):

```go
var hydeCmd = &cobra.Command{
	Use:   "hyde <query>",
	Short: "Two-level HyDE search (Level1 @hyde -> Level2 precision -> fallback hybrid)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.HyDE(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}
		if jsonOut {
			printBody(body)
			return nil
		}
		var resp struct {
			Hits   []formatter.SearchHit `json:"hits"`
			Routed bool                  `json:"routed"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return err
		}
		if !resp.Routed {
			fmt.Fprintf(os.Stderr, "hyde: no @hyde matches, fallback to global hybrid search\n")
		}
		return formatResults(os.Stdout, resp.Hits)
	},
}
```

- [ ] **Step 2: Update client.go HyDE** (add strategy param to match server):

```go
func (my *Client) HyDE(query, collection string, limit int, minScore float64) ([]byte, error) {
	return my.Post("/hyde", map[string]interface{}{
		"query":      query,
		"collection": collection,
		"limit":      limit,
		"min_score":  minScore,
	})
}
```

Also add `HyDE` to benchmark client calls in bench.go.

- [ ] **Step 3: Add hyde to bench backends**

```go
var benchBackends = []benchBackend{
	{"search", "/search", true},
	{"vsearch", "/vsearch", true},
	{"hybrid", "/hybrid", true},
	{"hyde", "/hyde", true},
}
```

- [ ] **Step 4: Build**

```bash
cd /Users/xmli/me/code/lmd && make build
```

- [ ] **Step 5: Commit**

```bash
git add internal/cli/search.go internal/daemon/client.go internal/cli/bench.go
git commit -m "feat: update hyde CLI and add to bench backends"
```

---

### Task 12: Clean up @summaries data

**Files:**
- New migration: SQL cleanup

- [ ] **Step 1: Delete @summaries collection**

```bash
cd /Users/xmli/me/code/lmd && ./lmd stop 2>/dev/null
sqlite3 ~/.cache/lmd/index.sqlite "
DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE doc_id IN (SELECT id FROM documents WHERE collection='@summaries'));
DELETE FROM chunks_vec WHERE chunk_id IN (SELECT id FROM chunks WHERE doc_id IN (SELECT id FROM documents WHERE collection='@summaries'));
DELETE FROM chunks WHERE doc_id IN (SELECT id FROM documents WHERE collection='@summaries');
DELETE FROM documents WHERE collection='@summaries';
DELETE FROM collections WHERE name='@summaries';
"
```

- [ ] **Step 2: Restart daemon**

```bash
nohup ./lmd daemon-start > /tmp/lmd.log 2>&1 &
sleep 5 && curl -s http://localhost:12345/status | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('collections', []))"
```

Expected: no `@summaries` collection in status

- [ ] **Step 3: Commit** (none — this is a runtime cleanup, not code)

---

### Task 13: End-to-end verification

- [ ] **Step 1: Run benchmark on covered 29 questions**

```bash
cd /Users/xmli/me/code/lmd && ./lmd bench longmemeval --limit 29 --mode hyde 2>&1 | tail -10
```

- [ ] **Step 2: Compare with hybrid baseline**

```bash
./lmd bench longmemeval --limit 29 --mode hybrid 2>&1 | tail -10
```

Expected: hyde R@10 >= hybrid R@10 (or at least within 5%)

- [ ] **Step 3: Verify fallback works**

```bash
curl -s -X POST http://localhost:12345/hyde -d '{"query":"test query with no match","limit":5}' | python3 -c "import json,sys; d=json.load(sys.stdin); print('routed:', d.get('routed'))"
```

Expected: `routed: false` (fallback to hybrid)

- [ ] **Step 4: Run full test suite**

```bash
go test -tags fts5 ./internal/... -count=1 2>&1 | tail -10
```

Expected: all PASS

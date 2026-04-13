# Phase 2 - Vector Search Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan.

**Goal:** Add vector embedding, sqlite-vec storage, and semantic search (vsearch) to LMD.

**Architecture:** Markdown-aware chunking splits documents into ~900 token pieces. GGUF embedding model generates vectors stored in sqlite-vec. vsearch command performs semantic similarity search.

**Tech Stack:** sqlite-vec (CGo via asg017/sqlite-vec-go-bindings/cgo), go-ego/gse, mattn/go-sqlite3

**Spec:** `docs/superpowers/specs/2026-04-12-lmd-design.md`

**Depends on:** Phase 1 (complete)

---

## Chunk 1: sqlite-vec Integration

### Task 1: Add sqlite-vec to store layer

**Files:**
- Modify: `go.mod` (add sqlite-vec dependency)
- Modify: `internal/store/db.go` (enable sqlite-vec)
- Modify: `internal/store/schema.go` (add chunk_vectors table to migration)
- Create: `internal/store/vector.go`
- Test: `internal/store/vector_test.go`

- [ ] **Step 1: Add sqlite-vec Go binding**

Run:
```bash
go get github.com/asg017/sqlite-vec-go-bindings/cgo@latest
```

- [ ] **Step 2: Write failing tests for vector operations**

Create `internal/store/vector_test.go`:
```go
package store

import (
	"testing"
)

func TestStoreAndQueryVectors(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	// Insert a chunk first (needed for FK)
	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc, "hello", "test")

	chunks, _ := InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
		{Content: "chunk two", Position: 10, TokenCount: 2, Hash: "c2"},
	})

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Store vectors
	vec1 := []float32{0.1, 0.2, 0.3, 0.4}
	vec2 := []float32{0.5, 0.6, 0.7, 0.8}

	err := InsertVector(db, chunks[0].ID, vec1)
	if err != nil {
		t.Fatalf("InsertVector failed: %v", err)
	}
	err = InsertVector(db, chunks[1].ID, vec2)
	if err != nil {
		t.Fatalf("InsertVector failed: %v", err)
	}

	// Query by vector
	query := []float32{0.1, 0.2, 0.3, 0.4}
	results, err := QueryVectors(db, query, 2)
	if err != nil {
		t.Fatalf("QueryVectors failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected vector search results")
	}
	if results[0].ChunkID != chunks[0].ID {
		t.Fatalf("expected closest match to be chunk 0, got chunk %d", results[0].ChunkID)
	}
}

func TestGetUnembeddedChunks(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc, "hello", "test")

	_, _ = InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
		{Content: "chunk two", Position: 10, TokenCount: 2, Hash: "c2"},
	})

	unembedded, err := GetUnembeddedChunks(db, "test-model")
	if err != nil {
		t.Fatalf("GetUnembeddedChunks failed: %v", err)
	}
	if len(unembedded) != 2 {
		t.Fatalf("expected 2 unembedded chunks, got %d", len(unembedded))
	}

	// Mark one as embedded
	_ = MarkEmbedded(db, unembedded[0].ID, "test-model")

	unembedded2, _ := GetUnembeddedChunks(db, "test-model")
	if len(unembedded2) != 1 {
		t.Fatalf("expected 1 unembedded after marking, got %d", len(unembedded2))
	}
}
```

- [ ] **Step 3: Run test — verify FAIL**

Run: `go test -tags "fts5" ./internal/store/ -run TestStoreAndQueryVectors -v`

- [ ] **Step 4: Enable sqlite-vec in db.go**

Add to `internal/store/db.go` imports:
```go
sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
```

Call `sqlite_vec.Auto()` before any `sql.Open`:
```go
func init() {
	sqlite_vec.Auto()
}
```

- [ ] **Step 5: Update schema to create chunk_vectors table**

In `internal/store/schema.go`, add to `migrateV1` stmts:
```sql
CREATE VIRTUAL TABLE IF NOT EXISTS chunk_vectors USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding float[1024]
)
```

- [ ] **Step 6: Implement vector.go**

Create `internal/store/vector.go`:
```go
package store

import (
	"database/sql"
	"math"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type ChunkData struct {
	Content    string
	Position   int
	TokenCount int
	Hash       string
}

type ChunkRecord struct {
	ID         int64
	DocID      int64
	Seq        int
	Content    string
	Position   int
	TokenCount int
	Hash       string
}

type VectorSearchResult struct {
	ChunkID  int64
	Distance float64
}

func InsertChunks(db *sql.DB, docID int64, chunks []ChunkData) ([]ChunkRecord, error) {
	var records []ChunkRecord
	for i, c := range chunks {
		res, err := db.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, ?, ?, ?, ?, ?)",
			docID, i, c.Content, c.Position, c.TokenCount, c.Hash,
		)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		records = append(records, ChunkRecord{
			ID: id, DocID: docID, Seq: i,
			Content: c.Content, Position: c.Position,
			TokenCount: c.TokenCount, Hash: c.Hash,
		})
	}
	return records, nil
}

func InsertVector(db *sql.DB, chunkID int64, embedding []float32) error {
	vec, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT INTO chunk_vectors(chunk_id, embedding) VALUES (?, ?)", chunkID, vec)
	return err
}

func DeleteVectorsByDocID(db *sql.DB, docID int64) error {
	rows, err := db.Query("SELECT id FROM chunks WHERE doc_id=?", docID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chunkID int64
		rows.Scan(&chunkID)
		db.Exec("DELETE FROM chunk_vectors WHERE chunk_id=?", chunkID)
	}
	_, err = db.Exec("DELETE FROM chunks WHERE doc_id=?", docID)
	return err
}

func QueryVectors(db *sql.DB, query []float32, limit int) ([]VectorSearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(query)
	if err != nil {
		return nil, err
	}

	// Pad/truncate query to match embedding dimension (1024)
	padded := make([]float32, 1024)
	copy(padded, query)
	q, err = sqlite_vec.SerializeFloat32(padded)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT chunk_id, distance
		FROM chunk_vectors
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkID, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func GetUnembeddedChunks(db *sql.DB, modelName string) ([]ChunkRecord, error) {
	rows, err := db.Query(`
		SELECT c.id, c.doc_id, c.seq, c.content, c.position, c.token_count, c.hash
		FROM chunks c
		WHERE c.id NOT IN (
			SELECT chunk_id FROM embed_status WHERE model_name = ?
		)
		ORDER BY c.id
	`, modelName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func MarkEmbedded(db *sql.DB, chunkID int64, modelName string) error {
	_, err := db.Exec(
		"INSERT OR IGNORE INTO embed_status (chunk_id, model_name) VALUES (?, ?)",
		chunkID, modelName,
	)
	return err
}

func GetChunksByDocID(db *sql.DB, docID int64) ([]ChunkRecord, error) {
	rows, err := db.Query(
		"SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE doc_id=? ORDER BY seq",
		docID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func SimilarityToScore(distance float64) float64 {
	return 1.0 / (1.0 + distance)
}

func NormalizeScore(score, maxScore float64) float64 {
	if maxScore == 0 {
		return 0
	}
	return math.Min(score/maxScore, 1.0)
}
```

- [ ] **Step 7: Run tests**

Run: `go test -tags "fts5" ./internal/store/ -v`

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: add sqlite-vec vector storage and chunk management"
```

---

## Chunk 2: Markdown-Aware Chunker

### Task 2: Implement Markdown chunker

**Files:**
- Create: `internal/chunker/chunker.go`
- Create: `internal/chunker/markdown.go`
- Test: `internal/chunker/markdown_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/chunker/markdown_test.go`:
```go
package chunker

import (
	"strings"
	"testing"
)

func TestChunkByHeading(t *testing.T) {
	text := "# Title\n\nParagraph one.\n\n## Section A\n\nContent A.\n\n## Section B\n\nContent B."
	chunks, err := NewMarkdownChunker(900).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for multi-section doc, got %d", len(chunks))
	}
}

func TestChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewMarkdownChunker(900).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short doc, got %d", len(chunks))
	}
}

func TestChunkEmptyDocument(t *testing.T) {
	chunks, err := NewMarkdownChunker(900).Chunk("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty doc, got %d", len(chunks))
	}
}

func TestChunkRespectsCodeBlocks(t *testing.T) {
	code := strings.Repeat("line\n", 100)
	text := "# Code\n\n```go\n" + code + "```\n\n## After\n\nMore content."
	chunks, err := NewMarkdownChunker(200).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	// Code block should not be split
	for _, c := range chunks {
		if strings.Contains(c.Content, "```") && strings.Count(c.Content, "```")%2 != 0 {
			t.Fatal("code block was split across chunks")
		}
	}
}

func TestChunkPosition(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph."
	chunks, err := NewMarkdownChunker(900).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) > 0 && chunks[0].Position != 0 {
		t.Fatalf("expected first chunk position=0, got %d", chunks[0].Position)
	}
}
```

- [ ] **Step 2: Implement chunker**

Create `internal/chunker/chunker.go`:
```go
package chunker

type Chunk struct {
	Content    string
	Position   int
	TokenCount int
}

type Chunker interface {
	Chunk(title string, body string) ([]Chunk, error)
}
```

Create `internal/chunker/markdown.go`:
```go
package chunker

import (
	"regexp"
	"strings"
)

type MarkdownChunker struct {
	MaxTokens int
}

func NewMarkdownChunker(maxTokens int) *MarkdownChunker {
	if maxTokens <= 0 {
		maxTokens = 900
	}
	return &MarkdownChunker{MaxTokens: maxTokens}
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+`)
var codeFenceRe = regexp.MustCompile("^```")
var hrRe = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

type breakPoint struct {
	pos   int
	score int
}

func (c *MarkdownChunker) Chunk(title string, body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	lines := strings.Split(body, "\n")
	var chunks []Chunk
	var current strings.Builder
	currentStart := 0
	inCodeBlock := false
	estTokens := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if codeFenceRe.MatchString(trimmed) {
			inCodeBlock = !inCodeBlock
		}

		if !inCodeBlock && current.Len() > 0 {
			score := c.breakScore(trimmed)
			if score > 0 && estTokens > c.MaxTokens*2/3 {
				content := current.String()
				chunks = append(chunks, Chunk{
					Content:    strings.TrimSpace(content),
					Position:   currentStart,
					TokenCount: estTokens,
				})
				current.Reset()
				currentStart = i
				estTokens = 0
			}
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
		estTokens += estimateTokens(line)
	}

	if current.Len() > 0 {
		content := current.String()
		chunks = append(chunks, Chunk{
			Content:    strings.TrimSpace(content),
			Position:   currentStart,
			TokenCount: estTokens,
		})
	}

	return chunks, nil
}

func (c *MarkdownChunker) breakScore(line string) int {
	if headingRe.MatchString(line) {
		level := len(regexp.MustCompile(`^#+`).FindString(line))
		switch {
		case level == 1:
			return 100
		case level == 2:
			return 90
		case level == 3:
			return 80
		default:
			return 70
		}
	}
	if hrRe.MatchString(line) {
		return 60
	}
	if line == "" {
		return 20
	}
	return 0
}

func estimateTokens(text string) int {
	// Rough estimate: Chinese chars ~1 token each, English words ~1 token each
	// Simple heuristic: count chars / 2 for mixed content
	return len(text) / 2
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/chunker/ -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add Markdown-aware document chunker"
```

---

## Chunk 3: Embedding Provider (Mock + Interface)

### Task 3: Embedding provider abstraction

Since GGUF integration via llama.cpp CGo is complex and requires C library compilation, we implement the interface first with a mock provider for testing, and defer the real GGUF provider to a follow-up.

**Files:**
- Create: `internal/embedding/provider.go`
- Create: `internal/embedding/mock.go`
- Test: `internal/embedding/mock_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/embedding/mock_test.go`:
```go
package embedding

import (
	"context"
	"testing"
)

func TestMockProviderEmbed(t *testing.T) {
	p := NewMockProvider(4)
	vec, err := p.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(vec))
	}
}

func TestMockProviderBatch(t *testing.T) {
	p := NewMockProvider(4)
	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestMockProviderDimension(t *testing.T) {
	p := NewMockProvider(128)
	if p.Dimension() != 128 {
		t.Fatalf("expected dimension 128, got %d", p.Dimension())
	}
}
```

- [ ] **Step 2: Implement provider interface + mock**

Create `internal/embedding/provider.go`:
```go
package embedding

import "context"

type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	ModelName() string
	Close() error
}
```

Create `internal/embedding/mock.go`:
```go
package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

type MockProvider struct {
	dim int
}

func NewMockProvider(dim int) *MockProvider {
	return &MockProvider{dim: dim}
}

func (m *MockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return m.textToVector(text), nil
}

func (m *MockProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = m.textToVector(t)
	}
	return vecs, nil
}

func (m *MockProvider) Dimension() int  { return m.dim }
func (m *MockProvider) ModelName() string { return "mock" }
func (m *MockProvider) Close() error      { return nil }

func (m *MockProvider) textToVector(text string) []float32 {
	vec := make([]float32, m.dim)
	h := sha256.Sum256([]byte(text))
	for i := range vec {
		b := h[i%len(h)]
		vec[i] = float32(b) / 256.0
	}
	// Normalize
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// Suppress unused import
var _ = binary.BigEndian
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/embedding/ -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add embedding provider interface with mock implementation"
```

---

## Chunk 4: Embedder Service + vsearch CLI

### Task 4: Embedder service

**Files:**
- Create: `internal/service/embedder.go`
- Test: `internal/service/embedder_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/service/embedder_test.go`:
```go
package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
)

func TestEmbedChunks(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(4)
	embedder := NewEmbedder(db, provider)

	result, err := embedder.EmbedAll("mock-model", false)
	if err != nil {
		t.Fatalf("EmbedAll failed: %v", err)
	}
	if result.Embedded == 0 {
		t.Fatal("expected some chunks to be embedded")
	}
}

func TestEmbedChunksIdempotent(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(4)
	embedder := NewEmbedder(db, provider)

	r1, _ := embedder.EmbedAll("mock-model", false)
	r2, _ := embedder.EmbedAll("mock-model", false)
	if r2.Embedded != 0 {
		t.Fatalf("second run should embed 0 (all done), got %d", r2.Embedded)
	}
	if r2.Skipped != r1.Embedded {
		t.Fatalf("expected %d skipped, got %d", r1.Embedded, r2.Skipped)
	}
}
```

NOTE: `setupIndexTest` and `newTestTokenizer` are already in `indexer_test.go` in the same package. But `newTestTokenizer` may not exist — check and add if needed:
```go
func newTestTokenizer(t *testing.T) *tokenizer.GseTokenizer {
	t.Helper()
	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
```

- [ ] **Step 2: Implement embedder**

Create `internal/service/embedder.go`:
```go
package service

import (
	"database/sql"
	"fmt"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
)

type EmbedResult struct {
	Embedded int
	Skipped  int
	Failed   int
}

type Embedder struct {
	db       *sql.DB
	provider embedding.EmbeddingProvider
}

func NewEmbedder(db *sql.DB, provider embedding.EmbeddingProvider) *Embedder {
	return &Embedder{db: db, provider: provider}
}

func (e *Embedder) EmbedAll(modelName string, force bool) (*EmbedResult, error) {
	result := &EmbedResult{}

	var chunks []store.ChunkRecord
	var err error

	if force {
		return nil, fmt.Errorf("force re-embed not yet implemented")
	}

	chunks, err = store.GetUnembeddedChunks(e.db, modelName)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		result.Skipped = 0
		return result, nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vecs, err := e.provider.EmbedBatch(nil, texts)
	if err != nil {
		return nil, err
	}

	for i, vec := range vecs {
		if err := store.InsertVector(e.db, chunks[i].ID, vec); err != nil {
			result.Failed++
			continue
		}
		if err := store.MarkEmbedded(e.db, chunks[i].ID, modelName); err != nil {
			result.Failed++
			continue
		}
		result.Embedded++
	}

	return result, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test -tags "fts5" ./internal/service/ -v`

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add embedder service for vector embedding"
```

### Task 5: Vector search service + vsearch CLI

**Files:**
- Modify: `internal/service/searcher.go` (add SearchVector)
- Modify: `internal/cli/search.go` (add vsearch command)

- [ ] **Step 1: Add SearchVector to searcher**

Add to `internal/service/searcher.go`:
```go
func (s *Searcher) SearchVector(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]SearchHit, error) {
	queryVec, err := provider.Embed(nil, query)
	if err != nil {
		return nil, err
	}

	vecResults, err := store.QueryVectors(s.db, queryVec, limit)
	if err != nil {
		return nil, err
	}

	var hits []SearchHit
	for _, r := range vecResults {
		score := store.SimilarityToScore(r.Distance)
		if score < minScore {
			continue
		}

		chunks, err := store.GetChunksByDocID(s.db, r.ChunkID)
		if err != nil || len(chunks) == 0 {
			continue
		}

		chunk := chunks[0]
		doc, err := store.GetDocumentByID(s.db, chunk.DocID)
		if err != nil {
			continue
		}

		if collection != "" && doc.Collection != collection {
			continue
		}

		hits = append(hits, SearchHit{
			DocID:      doc.DocID,
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      score,
			Snippet:    chunk.Content,
			Line:       1,
		})
	}

	if len(hits) > 0 {
		topScore := hits[0].Score
		for i := range hits {
			hits[i].Score = store.NormalizeScore(hits[i].Score, topScore)
		}
	}

	return hits, nil
}
```

Need to add `GetDocumentByID` to `internal/store/document.go`:
```go
func GetDocumentByID(db *sql.DB, id int64) (*DocumentRecord, error) {
	return getDocument(db, "WHERE id=?", id)
}
```

Need to fix `QueryVectors` — it queries by chunk_id but `GetChunksByDocID` takes docID. Fix: add `GetChunkByID`:
```go
func GetChunkByID(db *sql.DB, chunkID int64) (*ChunkRecord, error) {
	var c ChunkRecord
	err := db.QueryRow(
		"SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE id=?",
		chunkID,
	).Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash)
	if err == sql.ErrNoRows {
		return nil, errors.New("chunk not found")
	}
	return &c, err
}
```

Then SearchVector uses:
```go
chunk, err := store.GetChunkByID(s.db, r.ChunkID)
doc, err := store.GetDocumentByID(s.db, chunk.DocID)
```

- [ ] **Step 2: Add vsearch CLI command**

Add to `internal/cli/search.go`:
```go
var vsearchCmd = &cobra.Command{
	Use:   "vsearch <query>",
	Short: "Vector semantic search",
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
		results, err := searcher.SearchVector(provider, args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		for _, r := range results {
			fmt.Printf("%s:%d #%s\n", r.Path, r.Line, r.DocID)
			fmt.Printf("Title: %s\n", r.Title)
			fmt.Printf("Score: %.0f%%\n", r.Score*100)
			if searchFull {
				fmt.Println()
				fmt.Println(r.Snippet)
			} else {
				fmt.Printf("\n%s\n", r.Snippet)
			}
			fmt.Println()
		}
		return nil
	},
}
```

And in `init()`: `rootCmd.AddCommand(vsearchCmd)`

- [ ] **Step 3: Add embed CLI command**

Create `internal/cli/embed.go` or add to `internal/cli/index.go`:
```go
var embedForce bool

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Generate vector embeddings",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		provider := embedding.NewMockProvider(1024)
		embedder := service.NewEmbedder(db, provider)

		result, err := embedder.EmbedAll("mock", embedForce)
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
```

- [ ] **Step 4: Build and E2E test**

```bash
go build -tags "fts5" ./cmd/lmd/

rm -rf /tmp/lmd-phase2
mkdir -p /tmp/lmd-phase2/docs

cat > /tmp/lmd-phase2/docs/go.md << 'EOF'
# Go并发编程

Go语言通过goroutine和channel实现并发编程。
goroutine是轻量级线程，channel用于goroutine间通信。
EOF

cat > /tmp/lmd-phase2/docs/python.md << 'EOF'
# Python数据科学

Python是数据科学领域最流行的语言。
pandas和numpy是核心数据处理库。
EOF

./lmd --index /tmp/lmd-phase2/test.sqlite collection add /tmp/lmd-phase2/docs --name docs
./lmd --index /tmp/lmd-phase2/test.sqlite update
./lmd --index /tmp/lmd-phase2/test.sqlite embed
./lmd --index /tmp/lmd-phase2/test.sqlite vsearch "并发编程"
./lmd --index /tmp/lmd-phase2/test.sqlite status
```

- [ ] **Step 5: Run ALL tests**

Run: `go test -tags "fts5" ./... -v`

- [ ] **Step 6: Final commit**

```bash
git add -A
git commit -m "feat: complete Phase 2 - vector embedding and semantic search"
```

---

## Summary

Phase 2 adds:
- sqlite-vec vector storage via CGo bindings
- Markdown-aware document chunker
- Embedding provider interface (mock for now, GGUF deferred)
- Embedder service (generates vectors for chunks)
- Vector search (vsearch command)
- Embed CLI command

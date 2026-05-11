# Per-Doc Linear Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace three independent tickers (syncIndex + embedChunks + summarize) with a single per-doc linear pipeline: read → chunk → embed → summarize → write DB.

**Architecture:** One ticker, one goroutine. Scan detects filesystem changes, returns PendingDoc list. Process each doc linearly: embed chunks in batches, generate summary, embed summary, write everything to DB. Document record's `file_mod_time` field serves as completion marker (0 = incomplete, >0 = complete).

**Tech Stack:** Go, SQLite (FTS5 + vec0), existing embed/LLM providers.

**Spec:** `docs/superpowers/specs/2026-05-11-per-doc-pipeline-design.md`

---

## Task 1: DAO — DeleteDocumentAndSummary

**Files:**
- Modify: `internal/dao/document.go`
- Test: `internal/dao/document_test.go`

**Why:** Process phase needs to delete a document AND its associated summary (in `@summaries` collection) in one transaction. Currently `DeleteDocument` only deletes the document itself, not the summary.

- [ ] **Step 1: Write failing test**

In `internal/dao/document_test.go`, add:

```go
func TestDeleteDocumentAndSummary(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")
	mustAddCollection(t, "@summaries", "/data")

	doc := &DocumentRecord{Collection: "notes", Path: "a.md", Title: "A", Body: "body", Hash: "h1", FileSize: 4}
	dao.UpsertDocument(doc)

	chunks := []ChunkData{{Content: "chunk1", Position: 0, TokenCount: 1, Hash: "h1"}}
	dao.InsertChunks(doc.Id, chunks, []string{"chunk1"})

	summaryDoc := &DocumentRecord{Collection: "@summaries", Path: "/@summary/" + strconv.FormatInt(doc.Id, 10),
		Title: "Summary", Body: "summary text", Hash: "sh1", FileSize: 12, SourceDocId: doc.Id}
	dao.UpsertDocument(summaryDoc)
	summaryChunks := []ChunkData{{Content: "summary text", Position: 0, TokenCount: 2, Hash: "sh1"}}
	dao.InsertChunks(summaryDoc.Id, summaryChunks, []string{"summary text"})

	err := DeleteDocumentAndSummary(doc.Id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := GetDocumentById(doc.Id); err == nil {
		t.Fatal("original document should be deleted")
	}
	if _, err := GetDocumentBySourceDocId("@summaries", doc.Id); err == nil {
		t.Fatal("summary document should be deleted")
	}
	chunksAfter, _ := GetChunksByDocId(doc.Id)
	if len(chunksAfter) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunksAfter))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags "fts5" -count=1 -run TestDeleteDocumentAndSummary ./internal/dao/`
Expected: FAIL — `DeleteDocumentAndSummary` undefined

- [ ] **Step 3: Implement DeleteDocumentAndSummary**

In `internal/dao/document.go`, add:

```go
func DeleteDocumentAndSummary(docId int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		summaryRows, err := tx.Query("SELECT id FROM documents WHERE source_doc_id=?", docId)
		if err != nil {
			return err
		}
		var summaryIds []int64
		for summaryRows.Next() {
			var sid int64
			summaryRows.Scan(&sid)
			summaryIds = append(summaryIds, sid)
		}
		summaryRows.Close()

		for _, sid := range summaryIds {
			if err := deleteDocChunksAndVecs(tx, sid); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM documents WHERE id=?", sid); err != nil {
				return err
			}
		}

		if err := deleteDocChunksAndVecs(tx, docId); err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM documents WHERE id=?", docId)
		return err
	})
}

func deleteDocChunksAndVecs(tx *sql.Tx, docId int64) error {
	rows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", docId)
	if err != nil {
		return err
	}
	var chunkIds []int64
	for rows.Next() {
		var cid int64
		rows.Scan(&cid)
		chunkIds = append(chunkIds, cid)
	}
	rows.Close()

	for _, cid := range chunkIds {
		tx.Exec("DELETE FROM chunks_vec WHERE chunk_id=?", cid)
		tx.Exec("DELETE FROM chunks_fts WHERE rowid=?", cid)
	}
	_, err = tx.Exec("DELETE FROM chunks WHERE doc_id=?", docId)
	return err
}
```

Note: Extract the chunk+vector deletion logic from the existing `DeleteDocument` into `deleteDocChunksAndVecs`. Then rewrite `DeleteDocument` to use it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags "fts5" -count=1 -run TestDeleteDocumentAndSummary ./internal/dao/`
Expected: PASS

- [ ] **Step 5: Run full DAO tests**

Run: `go test -tags "fts5" -count=1 ./internal/dao/`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dao/document.go internal/dao/document_test.go
git commit -m "feat(dao): add DeleteDocumentAndSummary cascade delete"
```

---

## Task 2: DAO — InsertDocument + CompleteDocument

**Files:**
- Modify: `internal/dao/document.go`
- Test: `internal/dao/document_test.go`

**Why:** New pipeline inserts document first (to get docId for FK), then writes chunks/summary in batches, then marks document complete. `file_mod_time = 0` means "incomplete".

- [ ] **Step 1: Write failing test for InsertDocument**

```go
func TestInsertDocument(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, err := InsertDocument("notes", "test.md", "Test Title", "body content", 12, "hash123")
	if err != nil {
		t.Fatal(err)
	}
	if docId <= 0 {
		t.Fatalf("expected positive docId, got %d", docId)
	}

	doc, err := GetDocumentById(docId)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Hash != "hash123" {
		t.Fatalf("expected hash 'hash123', got '%s'", doc.Hash)
	}
	if doc.FileModTime != 0 {
		t.Fatalf("expected file_mod_time=0 (incomplete), got %d", doc.FileModTime)
	}
	if doc.Collection != "notes" || doc.Path != "test.md" {
		t.Fatalf("unexpected collection/path: %s/%s", doc.Collection, doc.Path)
	}
}
```

- [ ] **Step 2: Write failing test for CompleteDocument**

```go
func TestCompleteDocument(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, _ := InsertDocument("notes", "test.md", "Title", "body", 4, "hash1")

	err := CompleteDocument(docId, 1234567890)
	if err != nil {
		t.Fatal(err)
	}

	doc, _ := GetDocumentById(docId)
	if doc.FileModTime != 1234567890 {
		t.Fatalf("expected file_mod_time=1234567890, got %d", doc.FileModTime)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test -tags "fts5" -count=1 -run "TestInsertDocument|TestCompleteDocument" ./internal/dao/`
Expected: FAIL

- [ ] **Step 4: Implement InsertDocument**

```go
func InsertDocument(collection, path, title, body string, fileSize int64, hash string) (int64, error) {
	docId := generateDocId(collection, path, hash)
	result, err := WithExec(
		"INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, modified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, DATETIME('now','+8 hours'), DATETIME('now','+8 hours'), DATETIME('now','+8 hours'))",
		docId, collection, path, title, body, hash, fileSize,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}
```

- [ ] **Step 5: Implement CompleteDocument**

```go
func CompleteDocument(docId int64, fileModTime int64) error {
	_, err := WithExec(
		"UPDATE documents SET file_mod_time=?, updated_at=DATETIME('now','+8 hours') WHERE id=?",
		fileModTime, docId,
	)
	return err
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test -tags "fts5" -count=1 -run "TestInsertDocument|TestCompleteDocument" ./internal/dao/`
Expected: PASS

- [ ] **Step 7: Run full DAO tests**

Run: `go test -tags "fts5" -count=1 ./internal/dao/`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/dao/document.go internal/dao/document_test.go
git commit -m "feat(dao): add InsertDocument + CompleteDocument for two-phase doc write"
```

---

## Task 3: DAO — InsertChunksAndVectors

**Files:**
- Modify: `internal/dao/chunks_vec.go`
- Test: `internal/dao/chunks_test.go`

**Why:** New pipeline writes chunks + FTS + vectors in one transaction per batch. Currently these are separate operations.

- [ ] **Step 1: Write failing test**

```go
func TestInsertChunksAndVectors(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, _ := dao.InsertDocument("notes", "test.md", "Title", "body", 4, "h1")

	chunks := []ChunkData{
		{Content: "hello world", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "foo bar", Position: 1, TokenCount: 2, Hash: "h1"},
	}
	tokenized := []string{"hello world", "foo bar"}
	vecs := [][]float32{make([]float32, EmbeddingDim), make([]float32, EmbeddingDim)}
	vecs[0][0] = 0.5
	vecs[1][0] = 0.8

	inserted, err := InsertChunksAndVectors(docId, "notes", chunks, tokenized, vecs)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(inserted))
	}

	for _, c := range inserted {
		var vecCount int
		rows, _ := WithQuery("SELECT COUNT(*) FROM chunks_vec WHERE chunk_id=?", c.Id)
		if rows.Next() {
			rows.Scan(&vecCount)
		}
		rows.Close()
		if vecCount != 1 {
			t.Fatalf("chunk %d: expected 1 vector, got %d", c.Id, vecCount)
		}
	}

	ftsResults, _ := SearchFTS("hello", "notes", 10)
	if len(ftsResults) != 1 {
		t.Fatalf("expected 1 FTS result, got %d", len(ftsResults))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags "fts5" -count=1 -run TestInsertChunksAndVectors ./internal/dao/`
Expected: FAIL

- [ ] **Step 3: Implement InsertChunksAndVectors**

```go
type ChunkVecItem struct {
	ChunkId    int64
	DocId      int64
	Collection string
	Embedding  []float32
}

func InsertChunksAndVectors(docId int64, collection string, chunks []ChunkData, tokenized []string, vecs [][]float32) ([]ChunkRecord, error) {
	if len(chunks) != len(tokenized) || len(chunks) != len(vecs) {
		return nil, fmt.Errorf("chunks(%d), tokenized(%d), vecs(%d) length mismatch", len(chunks), len(tokenized), len(vecs))
	}

	var result []ChunkRecord
	err := withTransaction(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		ftsStmt, err := tx.Prepare("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)")
		if err != nil {
			return err
		}
		defer ftsStmt.Close()

		vecStmt, err := tx.Prepare("INSERT INTO chunks_vec (chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer vecStmt.Close()

		for i, c := range chunks {
			r, err := stmt.Exec(docId, i, c.Content, c.Position, c.TokenCount, c.Hash)
			if err != nil {
				return err
			}
			rowsAffected, _ := r.RowsAffected()
			if rowsAffected == 0 {
				continue
			}
			chunkId, _ := r.LastInsertId()

			ftsStmt.Exec(chunkId, tokenized[i])

			blob := sqlite_vec.SerializeFloat32(padVector(vecs[i]))
			vecStmt.Exec(chunkId, blob, docId, collection)

			result = append(result, ChunkRecord{
				Id: chunkId, DocId: docId, Seq: i,
				Content: c.Content, Position: c.Position,
				TokenCount: c.TokenCount, Hash: c.Hash,
			})
		}
		return nil
	})
	return result, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags "fts5" -count=1 -run TestInsertChunksAndVectors ./internal/dao/`
Expected: PASS

- [ ] **Step 5: Run full DAO tests**

Run: `go test -tags "fts5" -count=1 ./internal/dao/`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dao/chunks_vec.go internal/dao/chunks_test.go
git commit -m "feat(dao): add InsertChunksAndVectors for combined chunk+FTS+vector write"
```

---

## Task 4: Data Types + Indexer.ScanChanges

**Files:**
- Modify: `internal/service/indexer.go`
- Modify: `internal/service/indexer_test.go`

**Why:** Indexer currently writes chunks to DB during scan. Rewrite to return `[]PendingDoc` without writing anything.

- [ ] **Step 1: Define PendingDoc type**

At the top of `internal/service/indexer.go`, add:

```go
type DocAction int

const (
	DocNew DocAction = iota
	DocChanged
	DocDeleted
)

type PendingDoc struct {
	Action      DocAction
	Collection  string
	Path        string
	Title       string
	Body        string
	Hash        string
	FileSize    int64
	FileModTime int64
	OldDocId    int64
	Chunks      []dao.ChunkData
}
```

- [ ] **Step 2: Write failing test for ScanChanges**

The test should verify that ScanChanges detects new, changed, and deleted files, returning PendingDoc structs without writing chunks to DB.

```go
func TestScanChanges(t *testing.T) {
	// Setup: create temp dir with files, init DB with existing docs
	// Call ScanChanges
	// Verify it returns PendingDoc with correct Actions
	// Verify NO chunks were written to DB
}
```

Write this test in detail based on the existing `TestIndexCollection` pattern, checking:
- New file → `PendingDoc{Action: DocNew, Chunks: [...]}`
- Changed file → `PendingDoc{Action: DocChanged, OldDocId: ..., Chunks: [...]}`
- Deleted file → `PendingDoc{Action: DocDeleted, OldDocId: ...}`
- No chunks in DB for new/changed files (ScanChanges doesn't write)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test -tags "fts5" -count=1 -run TestScanChanges ./internal/service/`
Expected: FAIL

- [ ] **Step 4: Implement ScanChanges**

Rewrite the core of `UpdateCollection` into a new method `ScanChanges` that:
1. Reads DB for existing docs (same as current)
2. Walks filesystem (same as current)
3. For new/changed files: reads content, chunks, builds PendingDoc
4. For deleted files: builds PendingDoc{DocDeleted}
5. Returns `[]PendingDoc`
6. Does NOT call `UpsertDocument`, `InsertChunks`, `DeleteDocument`, or `DeleteVectorsByDocId`

Keep `UpdateCollection` as-is for backward compatibility until Task 7 (cleanup). The new method is additive.

- [ ] **Step 5: Run tests**

Run: `go test -tags "fts5" -count=1 ./internal/service/`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/service/indexer.go internal/service/indexer_test.go
git commit -m "feat(indexer): add ScanChanges returning PendingDoc without DB writes"
```

---

## Task 5: Processor — per-doc pipeline

**Files:**
- Create: `internal/service/processor.go`
- Create: `internal/service/processor_test.go`

**Why:** New component that processes one PendingDoc: embed chunks → summarize → embed summary → write DB.

- [ ] **Step 1: Write failing test**

```go
func TestProcessNewDoc(t *testing.T) {
	// Setup: init DB, add collection
	// Create PendingDoc with Action=DocNew, Chunks, Body, etc.
	// Call ProcessDoc(ctx, doc, embedProvider, llmProvider, chunker)
	// Verify:
	//   - Document exists in DB with correct hash and file_mod_time > 0
	//   - Chunks exist with vectors in chunks_vec
	//   - Summary doc exists in @summaries with vector
	//   - LLM was called once (summary)
	//   - EmbedProvider was called (for chunks + summary)
}

func TestProcessChangedDoc(t *testing.T) {
	// Setup: insert old document with chunks + summary
	// Create PendingDoc with Action=DocChanged, OldDocId set
	// Call ProcessDoc
	// Verify old data deleted, new data written
}

func TestProcessDeletedDoc(t *testing.T) {
	// Setup: insert document with chunks + summary
	// Create PendingDoc{Action: DocDeleted, OldDocId: ...}
	// Call ProcessDoc
	// Verify all data deleted
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags "fts5" -count=1 -run "TestProcess" ./internal/service/`
Expected: FAIL

- [ ] **Step 3: Implement Processor**

Create `internal/service/processor.go`:

```go
package service

type Processor struct {
	embedProvider embedding.EmbeddingProvider
	llm           llm.LLMProvider
	chunker       Chunker
	maxInput      int
	maxOutput     int
	tokenizer     tokenizer.Tokenizer
}

func NewProcessor(embedProv embedding.EmbeddingProvider, llmProv llm.LLMProvider, ch Chunker, tok tokenizer.Tokenizer, cfg config.SummaryConfig) *Processor {
	return &Processor{
		embedProvider: embedProv,
		llm:           llmProv,
		chunker:       ch,
		tokenizer:     tok,
		maxInput:      cfg.MaxInputTokens,
		maxOutput:     cfg.MaxOutputTokens,
	}
}

func (my *Processor) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	switch doc.Action {
	case DocDeleted:
		return dao.DeleteDocumentAndSummary(doc.OldDocId)
	case DocNew, DocChanged:
		return my.processNewOrChanged(ctx, doc)
	}
	return nil
}

func (my *Processor) processNewOrChanged(ctx context.Context, doc PendingDoc) error {
	if doc.Action == DocChanged {
		if err := dao.DeleteDocumentAndSummary(doc.OldDocId); err != nil {
			return err
		}
	}

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.Hash)
	if err != nil {
		return err
	}

	// Embed chunks in batches
	batchSize := 20
	for i := 0; i < len(doc.Chunks); i += batchSize {
		end := i + batchSize
		if end > len(doc.Chunks) {
			end = len(doc.Chunks)
		}
		batch := doc.Chunks[i:end]
		texts := make([]string, len(batch))
		tokenized := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
			tokenized[j] = my.tokenize(c.Content)
		}
		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed chunks batch %d: %w", i/batchSize, err)
		}
		if _, err := dao.InsertChunksAndVectors(docId, doc.Collection, batch, tokenized, vecs); err != nil {
			return fmt.Errorf("insert chunks batch %d: %w", i/batchSize, err)
		}
	}

	// Summarize
	summary, err := my.generateSummary(ctx, doc.Title, doc.Body)
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}

	// Embed summary
	summaryVecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
	if err != nil {
		return fmt.Errorf("embed summary: %w", err)
	}

	// Write summary
	tokenizedSummary := my.tokenize(summary)
	if err := dao.InsertSummaryWithVector(docId, summary, tokenizedSummary, summaryVecs[0]); err != nil {
		return fmt.Errorf("insert summary: %w", err)
	}

	// Mark document complete
	return dao.CompleteDocument(docId, doc.FileModTime)
}
```

Include `generateSummary`, `truncateContent`, `tokenize` helper methods (move from summarizer.go).

Note: `dao.InsertSummaryWithVector` needs updating — currently it takes `(sourceDocId, hash, summary, tokenized, vec)` but in the new design the hash and other params come from different places. Adjust the signature to match the new usage.

- [ ] **Step 4: Run tests**

Run: `go test -tags "fts5" -count=1 -run "TestProcess" ./internal/service/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/processor.go internal/service/processor_test.go
git commit -m "feat(processor): per-doc linear pipeline — embed + summarize + write"
```

---

## Task 6: Daemon — new goLoop

**Files:**
- Modify: `internal/daemon/daemon.go`

**Why:** Replace two-ticker goLoop with single ticker that scans and processes sequentially.

- [ ] **Step 1: Rewrite goLoop**

Replace current goLoop with:

```go
func (my *Daemon) goLoop(later loom.Later) {
	processor := service.NewProcessor(my.embedProvider, my.llmProvider, my.chunker, my.tokenizer, my.cfg.Summary)
	closeChan := my.wc.C()
	pipelineTicker := later.NewTicker(indexSyncInterval)

	for {
		select {
		case <-closeChan:
			return
		case <-pipelineTicker.C:
			pending := my.scanChanges()
			for _, doc := range pending {
				select {
				case <-closeChan:
					return
				default:
				}
				ctx := context.Background()
				if err := processor.ProcessDoc(ctx, doc); err != nil {
					logo.Warn("pipeline: process %s/%s failed: %s", doc.Collection, doc.Path, err)
				}
			}
		}
	}
}

func (my *Daemon) scanChanges() []service.PendingDoc {
	my.rebuildMu.RLock()
	defer my.rebuildMu.RUnlock()

	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("pipeline: list collections failed: %s", err)
		return nil
	}

	var pending []service.PendingDoc
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue
		}
		result, err := my.indexer.ScanChanges(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("pipeline: scan %s failed: %s", col.Name, err)
			continue
		}
		if len(result) > 0 {
			logo.Info("pipeline: %s has %d pending docs", col.Name, len(result))
		}
		pending = append(pending, result...)
	}
	return pending
}
```

- [ ] **Step 2: Remove old code**

Remove from daemon.go:
- `embedChunks()` method
- `embedTicker` from goLoop (if still present)
- `my.embedder` field from Daemon struct
- `my.summarizer` references (already removed)

Add `my.chunker` field if not present (processor needs it).

- [ ] **Step 3: Run build + tests**

Run: `go build -tags "fts5" ./... && go test -tags "fts5" -count=1 ./internal/daemon/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "refactor(daemon): single-ticker pipeline with scan+process loop"
```

---

## Task 7: Cleanup — delete old code

**Files:**
- Delete: `internal/service/embedder.go`
- Delete: `internal/service/summarizer.go`
- Delete: `internal/service/summarizer_test.go`
- Delete: `internal/service/embedder_test.go` (if exists)
- Modify: `internal/dao/stats.go` — remove `GetUnembeddedCount`
- Modify: `internal/dao/chunks_vec.go` — remove `GetUnembeddedChunks`
- Modify: `internal/dao/document.go` — remove `UpsertSummaryDoc` (replaced by new `InsertSummaryWithVector`), remove `TouchDocument`
- Modify: `internal/service/indexer.go` — remove old `UpdateCollection` method, keep only `ScanChanges`
- Modify: any remaining references to deleted code

- [ ] **Step 1: Delete files**

```bash
rm internal/service/embedder.go internal/service/summarizer.go internal/service/summarizer_test.go
```

- [ ] **Step 2: Remove unused DAO methods**

Remove:
- `GetUnembeddedChunks` from `chunks_vec.go`
- `GetUnembeddedCount` from `stats.go`
- `TouchDocument` from `document.go`
- Old `UpsertSummaryDoc` from `document.go` (keep only `InsertSummaryWithVector` or new version)

- [ ] **Step 3: Remove old UpdateCollection from indexer**

Remove the `UpdateCollection` method from `indexer.go`. Keep only `ScanChanges` and its helpers.

- [ ] **Step 4: Fix compilation errors**

Search for references to deleted methods and fix:
```bash
rg "UpdateCollection|EmbedBatch|GetUnembeddedChunks|GetUnembeddedCount|TouchDocument|UpsertSummaryDoc" --type go
```

- [ ] **Step 5: Run full test suite**

Run: `go build -tags "fts5" ./... && go test -tags "fts5" -count=1 ./...`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: delete old Embedder, Summarizer, and unused DAO methods"
```

---

## Task 8: Final integration test

**Files:**
- Test: `internal/service/processor_test.go`

**Why:** End-to-end test that simulates the full pipeline: add files → scan → process → verify search works.

- [ ] **Step 1: Write integration test**

Test the complete flow:
1. Create temp dir with markdown files
2. Add collection pointing to temp dir
3. Scan changes → get PendingDocs
4. Process each doc
5. Verify: FTS search finds results, vector search finds results, summary exists

- [ ] **Step 2: Write crash recovery test**

1. Insert document with file_mod_time=0 (incomplete)
2. Scan → should detect incomplete doc
3. Process → should delete incomplete data and redo

- [ ] **Step 3: Run full test suite**

Run: `go build -tags "fts5" ./... && go test -tags "fts5" -count=1 ./...`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/processor_test.go
git commit -m "test(processor): add integration and crash recovery tests"
```

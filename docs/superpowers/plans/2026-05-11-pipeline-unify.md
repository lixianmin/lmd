# Pipeline 统一 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 daemon 的三个独立 ticker (syncIndex/summaryTicker/embedTicker) 重构为 pipelineTicker(syncIndex→ProcessDirty) + embedTicker 并行，并将 summarize+embed 合并为事务写入。

**Architecture:** Summarizer 持有 embedProvider 引用，processDoc 中先调 LLM 生成摘要，再调 Embed API 生成向量，最后通过新增的 DAO 方法 `UpsertSummaryWithVector` 在一个事务内写入 summary doc + chunk + FTS + vector。goLoop 中删除 summaryTicker，改为 pipelineTick 串行执行 syncIndex→ProcessDirty；embedTicker 保留，独立并行。

**Tech Stack:** Go, SQLite (WAL, FTS5, vec0), mattn/go-sqlite3 (build tag: fts5)

---

### Task 1: 新增 DAO 方法 UpsertSummaryWithVector

**Files:**
- Modify: `internal/dao/document.go:228-283` (在 `UpsertSummaryDoc` 之后新增方法)
- Test: `internal/dao/document_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/dao/document_test.go` 中新增测试函数：

```go
func TestUpsertSummaryWithVector(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data", "**/*.md", nil)

	doc := &DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test",
		Body: "body", Hash: "hash1", FileSize: 4,
	}
	if err := UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	vec := make([]float32, 1024)
	for i := range vec {
		vec[i] = float32(i % 100)
	}

	docId, err := UpsertSummaryWithVector(doc.Id, "hash1", "summary text", "summary text", vec)
	if err != nil {
		t.Fatalf("UpsertSummaryWithVector: %v", err)
	}
	if docId == 0 {
		t.Fatal("expected non-zero docId")
	}

	got, err := GetDocumentBySourceDocId("@summaries", doc.Id)
	if err != nil {
		t.Fatalf("GetDocumentBySourceDocId: %v", err)
	}
	if got.SourceDocId != doc.Id {
		t.Fatalf("expected source_doc_id=%d, got %d", doc.Id, got.SourceDocId)
	}

	chunks, _ := GetChunksByDocId(got.Id)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "summary text" {
		t.Fatalf("expected 'summary text', got '%s'", chunks[0].Content)
	}

	count := getVectorCount(t, chunks[0].Id)
	if count != 1 {
		t.Fatalf("expected 1 vector for chunk %d, got %d", chunks[0].Id, count)
	}
}

func getVectorCount(t *testing.T, chunkId int64) int {
	t.Helper()
	var count int
	err := DB.db.QueryRow("SELECT COUNT(*) FROM chunks_vec WHERE chunk_id=?", chunkId).Scan(&count)
	if err != nil {
		t.Fatalf("count vectors: %v", err)
	}
	return count
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -tags "fts5" -count=1 -run TestUpsertSummaryWithVector ./internal/dao/`
Expected: FAIL — `UpsertSummaryWithVector` undefined

- [ ] **Step 3: 实现 UpsertSummaryWithVector**

在 `internal/dao/document.go` 的 `UpsertSummaryDoc` 之后新增：

```go
func UpsertSummaryWithVector(sourceDocId int64, hash, summary, tokenizedSummary string, vec []float32) (int64, error) {
	var docId int64
	err := withTransaction(func(tx *sql.Tx) error {
		existingRows, err := tx.Query("SELECT id FROM documents WHERE collection='@summaries' AND source_doc_id=?", sourceDocId)
		if err != nil {
			return err
		}
		var existingIds []int64
		for existingRows.Next() {
			var eid int64
			if err := existingRows.Scan(&eid); err != nil {
				existingRows.Close()
				return err
			}
			existingIds = append(existingIds, eid)
		}
		existingRows.Close()

		for _, did := range existingIds {
			if _, err := tx.Exec("DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE doc_id=?)", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks_vec WHERE chunk_id IN (SELECT id FROM chunks WHERE doc_id=?)", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks WHERE doc_id=?", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM documents WHERE id=?", did); err != nil {
				return err
			}
		}

		docIdStr := generateDocId("@summaries", fmt.Sprintf("%d", sourceDocId), hash)
		res, err := tx.Exec(`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, modified_at)
			VALUES (?, '@summaries', ?, '', '', ?, 0, 0, ?, DATETIME('now', '+8 hours'))`,
			docIdStr, fmt.Sprintf("/@summary/%d", sourceDocId), hash, sourceDocId)
		if err != nil {
			return err
		}
		docId, _ = res.LastInsertId()

		chunkRes, err := tx.Exec("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, 0, ?, 0, 0, ?)", docId, summary, hash)
		if err != nil {
			return err
		}
		chunkId, _ := chunkRes.LastInsertId()

		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, tokenizedSummary)
		if err != nil {
			return err
		}

		serialized, err := sqlite_vec.SerializeFloat32(padVector(vec))
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO chunks_vec(chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, '@summaries')", chunkId, serialized, docId)
		return err
	})
	return docId, err
}
```

注意：需要在 `document.go` 的 import 中添加 `sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"`。同时检查 `padVector` 是否在 `chunks_vec.go` 中已导出（它在同包内，可直接使用）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -tags "fts5" -count=1 -run TestUpsertSummaryWithVector ./internal/dao/`
Expected: PASS

- [ ] **Step 5: 写幂等测试（重复调用不报错）**

```go
func TestUpsertSummaryWithVectorIdempotent(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data", "**/*.md", nil)

	doc := &DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test",
		Body: "body", Hash: "hash1", FileSize: 4,
	}
	UpsertDocument(doc)

	vec := make([]float32, 1024)

	docId1, _ := UpsertSummaryWithVector(doc.Id, "hash1", "summary v1", "summary v1", vec)
	docId2, _ := UpsertSummaryWithVector(doc.Id, "hash2", "summary v2", "summary v2", vec)

	got, _ := GetDocumentBySourceDocId("@summaries", doc.Id)
	if got.Id != docId2 {
		t.Fatalf("expected docId=%d (second upsert), got %d", docId2, got.Id)
	}

	chunks, _ := GetChunksByDocId(got.Id)
	if chunks[0].Content != "summary v2" {
		t.Fatalf("expected 'summary v2', got '%s'", chunks[0].Content)
	}

	// 旧 doc 的 chunks 不应残留
	count := 0
	DB.db.QueryRow("SELECT COUNT(*) FROM chunks WHERE doc_id=?", docId1).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 chunks for old docId %d, got %d", docId1, count)
	}
}
```

- [ ] **Step 6: 运行全部 dao 测试确认无回归**

Run: `go test -tags "fts5" -count=1 ./internal/dao/`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/dao/document.go internal/dao/document_test.go
git commit -m "feat(dao): add UpsertSummaryWithVector for atomic summary+vector write"
```

---

### Task 2: 重构 Summarizer — 注入 embedProvider，合并 embed 到 processDoc

**Files:**
- Modify: `internal/service/summarizer.go`
- Modify: `internal/service/summarizer_test.go`

- [ ] **Step 1: 写失败测试 — processDoc 同时生成 summary 和 vector**

```go
func TestSummarizerProcessDocWithEmbedding(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test Doc",
		Body: "body text", Hash: "hash1", FileSize: 9,
	}
	if err := dao.UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	tokenized := []string{"hello world this is a test chunk"}
	chunks := []dao.ChunkData{
		{Content: "hello world this is a test chunk", Position: 0, TokenCount: 6, Hash: "h1"},
	}
	if _, err := dao.InsertChunks(doc.Id, chunks, tokenized); err != nil {
		t.Fatal(err)
	}

	mockLLM := llm.NewMockLLM("这是一个测试文档的摘要")
	mockEmbed := embedding.NewMockProvider(1024)
	cfg := config.SummaryConfig{
		MaxInputTokens:  30000,
		MaxOutputTokens: 200,
	}
	s := NewSummarizer(mockLLM, cfg, nil, mockEmbed)

	if err := s.processDoc(context.Background(), doc.Id); err != nil {
		t.Fatalf("processDoc: %v", err)
	}

	if mockLLM.Called != 1 {
		t.Fatalf("expected LLM called once, got %d", mockLLM.Called)
	}

	got, err := dao.GetDocumentBySourceDocId("@summaries", doc.Id)
	if err != nil {
		t.Fatalf("GetDocumentBySourceDocId: %v", err)
	}
	if got.SourceDocId != doc.Id {
		t.Fatalf("expected source_doc_id=%d, got %d", doc.Id, got.SourceDocId)
	}

	chunksAfter, _ := dao.GetChunksByDocId(got.Id)
	if len(chunksAfter) != 1 {
		t.Fatalf("expected 1 summary chunk, got %d", len(chunksAfter))
	}
	if chunksAfter[0].Content != "这是一个测试文档的摘要" {
		t.Fatalf("expected mock summary, got '%s'", chunksAfter[0].Content)
	}

	var vecCount int
	dao.DB.db.QueryRow("SELECT COUNT(*) FROM chunks_vec WHERE chunk_id=?", chunksAfter[0].Id).Scan(&vecCount)
	if vecCount != 1 {
		t.Fatalf("expected 1 vector for summary chunk, got %d", vecCount)
	}
}
```

注意：此测试需要 `import "github.com/lixianmin/lmd/internal/embedding"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -tags "fts5" -count=1 -run TestSummarizerProcessDocWithEmbedding ./internal/service/`
Expected: FAIL — `NewSummarizer` 参数不匹配或编译错误

- [ ] **Step 3: 修改 Summarizer 结构体和构造函数**

修改 `internal/service/summarizer.go`：

1. 在 import 中添加 `"github.com/lixianmin/lmd/internal/embedding"`
2. 结构体添加 `embedProvider embedding.EmbeddingProvider` 字段，删除 `onUpsert func()` 和 `stopCh <-chan struct{}` 字段
3. 构造函数签名改为：

```go
func NewSummarizer(llmProvider llm.LLMProvider, cfg config.SummaryConfig, tok tokenizer.Tokenizer, embedProv embedding.EmbeddingProvider) *Summarizer {
	var my = &Summarizer{
		dirty:        make(map[int64]bool),
		llm:          llmProvider,
		maxOutput:    cfg.MaxOutputTokens,
		maxInput:     cfg.MaxInputTokens,
		tokenizer:    tok,
		embedProvider: embedProv,
	}
	return my
}
```

4. 删除 `SetOnUpsert` 和 `SetStopCh` 方法

- [ ] **Step 4: 修改 processDoc — 合并 embed 调用**

替换 `processDoc` 中 LLM 调用后的逻辑。原代码从 `summary, err := my.generateSummary(...)` 开始到 `my.upsertSummary(docId, doc.Hash, summary)` 和 `onUpsert` 回调，替换为：

```go
	summary, err := my.generateSummary(ctx, doc.Title, content)
	if err != nil {
		logo.Warn("summarizer: generate summary for doc %d error: %s", docId, err)
		return err
	}

	vecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
	if err != nil {
		logo.Warn("summarizer: embed summary for doc %d error: %s", docId, err)
		return err
	}

	tokenized := summary
	if my.tokenizer != nil {
		if t := my.tokenizer.TokenizeToString(summary); t != "" {
			tokenized = t
		}
	}

	logo.Info("summarizer: generated summary for doc %d (%s) → %s", docId, doc.Title, summary)
	my.upsertSummaryWithVector(docId, doc.Hash, summary, tokenized, vecs[0])
	return nil
```

- [ ] **Step 5: 修改 upsertSummary 为 upsertSummaryWithVector**

将 `upsertSummary` 方法替换为：

```go
func (my *Summarizer) upsertSummaryWithVector(sourceDocId int64, hash, summary, tokenizedSummary string, vec []float32) {
	docId, err := dao.UpsertSummaryWithVector(sourceDocId, hash, summary, tokenizedSummary, vec)
	if err != nil {
		logo.Error("summarizer: upsert summary with vector for doc %d failed: %s", sourceDocId, err)
	} else {
		logo.Info("summarizer: upserted summary+vector for sourceDoc %d → summaryDoc %d", sourceDocId, docId)
	}
}
```

- [ ] **Step 6: 修改 ProcessDirty 签名，增加 ctx 参数**

```go
func (my *Summarizer) ProcessDirty(ctx context.Context) {
	dirty := my.popDirty()
	if len(dirty) == 0 {
		return
	}

	logo.Info("summarizer: processing %d dirty docs", len(dirty))
	var done, failed int
	for docId := range dirty {
		if err := my.processDoc(ctx, docId); err != nil {
			failed++
		} else {
			done++
		}
	}
	logo.Info("summarizer: done processing %d docs (%d ok, %d failed)", len(dirty), done, failed)
}
```

删除内部创建 ctx + stopCh 监听的代码。

- [ ] **Step 7: 更新所有现有测试**

1. 所有 `NewSummarizer(mockLLM, cfg, nil)` 调用改为 `NewSummarizer(mockLLM, cfg, nil, nil)`
   - 但 `TestSummarizerProcessDocWithEmbedding` 传 `mockEmbed`
   - 其他测试传 `nil`（embedProvider 为 nil 时 processDoc 中 EmbedBatch 会 panic — 但这些测试不会走到 embed 步骤，因为它们要么测 skip 逻辑，要么 mock LLM 返回 error 后就 return 了）
   - **注意：** `TestSummarizerProcessDoc` 会走到 embed 调用（LLM 成功返回），所以这个测试也需要传 `mockEmbed`

2. 删除 `TestSummarizerOnUpsertCallback` — onUpsert 回调已删除

3. `TestSummarizerProcessDoc` 需要改为传入 `mockEmbed` 并验证 vector 存在（或者可以简化为只验证 summary chunk 仍然存在，因为新方法里 summary 和 vector 是一起写的）

修改 `TestSummarizerProcessDoc`：改 `NewSummarizer(mockLLM, cfg, nil)` 为 `NewSummarizer(mockLLM, cfg, nil, embedding.NewMockProvider(1024))`，添加 `import "github.com/lixianmin/lmd/internal/embedding"`。测试逻辑不变（已有验证 summary chunk 内容），但内部走的是新路径。

- [ ] **Step 8: 运行测试确认通过**

Run: `go test -tags "fts5" -count=1 ./internal/service/ -run TestSummarizer`
Expected: ALL PASS

- [ ] **Step 9: 运行全部 service 测试确认无回归**

Run: `go test -tags "fts5" -count=1 ./internal/service/`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/service/summarizer.go internal/service/summarizer_test.go
git commit -m "refactor(summarizer): inline embed into processDoc, remove onUpsert callback"
```

---

### Task 3: 重构 daemon.go — pipelineTick + 删除 summaryTicker/cooldown

**Files:**
- Modify: `internal/daemon/daemon.go:81-114` (Start 方法中的初始化和回调)
- Modify: `internal/daemon/daemon.go:201-225` (goLoop 方法)
- Modify: `internal/config/config.go` (删除 CooldownSeconds)
- Test: `internal/daemon/daemon_test.go`

- [ ] **Step 1: 修改 Summarizer 构造调用**

在 `daemon.go` Start 方法中（约 L106），修改 `NewSummarizer` 调用：

```go
my.summarizer = service.NewSummarizer(my.llmProvider, my.cfg.Summary, my.tokenizer, my.embedProvider)
```

- [ ] **Step 2: 删除 SetOnUpsert 和 SetStopCh 调用**

删除 L107-114 中以下代码：

```go
my.summarizer.SetOnUpsert(func() {
    loom.Go(func(later loom.Later) {
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        my.embedder.EmbedBatch(ctx, 1)
    })
})
```

以及 `my.summarizer.SetStopCh(my.stopCh)` — 但先确认 `stopCh` 是否还在别处使用。如果 `stopCh` 只是给 summarizer 用的，可以一并删除。

检查 `daemon.go` 中 `stopCh` 的所有使用：
- `my.stopCh` 在 Daemon 结构体中不存在（当前代码中 stopCh 是通过 `SetStopCh` 传入的）
- summarizer 的 stopCh 是 daemon 的 `my.wc.C()` — 实际上不是。重新查看... 当前代码没有看到 `my.stopCh` 字段，`SetStopCh` 传入的是 `my.stopCh`，但在 Daemon 结构体中未定义这个字段

实际上 `SetStopCh` 传入的 stopCh 来源需要确认。在当前代码中搜索：daemon.go 中没有 `stopCh` 字段。这个 channel 可能来自 `my.wc.C()`。

**结论：** 删除 `SetStopCh` 和 `SetOnUpsert` 调用。ProcessDirty 的 context 改为从 pipelineTick 中传入。

- [ ] **Step 3: 重写 goLoop**

替换 `goLoop` 方法（L201-225）：

```go
func (my *Daemon) goLoop(later loom.Later) {
	var pipelineTicker = later.NewTicker(indexSyncInterval)
	var embedTicker = later.NewTicker(embedTickInterval)
	var closeChan = my.wc.C()

	for {
		select {
		case <-closeChan:
			return
		case <-pipelineTicker.C:
			my.pipelineTick()
		case <-embedTicker.C:
			my.embedChunks()
		}
	}
}

func (my *Daemon) pipelineTick() {
	my.syncIndex()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	my.summarizer.ProcessDirty(ctx)
}
```

注意：`10*time.Minute` 是 ProcessDirty 的整体超时（处理多个 dirty doc），单个 doc 的 LLM 超时由 LLM provider 自身控制。

- [ ] **Step 4: 删除启动时的延迟 ScanAll goroutine**

当前代码（L115-123）中有一个延迟 `ScanAll()` 调用。这个应保留，它负责启动时恢复缺失的 summary：

```go
var closeChan = my.wc.C()
loom.Go(func(later loom.Later) {
    select {
    case <-closeChan:
        return
    case <-time.After(indexSyncInterval + 2*time.Second):
    }
    my.summarizer.ScanAll()
})
```

保持不变。

- [ ] **Step 5: 删除 config 中的 CooldownSeconds**

在 `internal/config/config.go` 中：
1. 从 `SummaryConfig` 结构体删除 `CooldownSeconds int` 字段
2. 从 `DefaultConfig()` 中删除 `CooldownSeconds: 60`

- [ ] **Step 6: 运行全部测试确认无回归**

Run: `make test`
Expected: ALL PASS

如果 `go vet` 或编译报错是因为其他地方引用了 `CooldownSeconds`，搜索并清理。

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/daemon.go internal/config/config.go
git commit -m "refactor(daemon): unify pipeline, remove summaryTicker and onUpsert callback"
```

---

### Task 4: 清理残留代码

**Files:**
- Modify: `internal/service/summarizer.go` — 确认无残留
- Modify: `internal/daemon/daemon.go` — 确认无残留
- Search: 全项目搜索被删除的 API 引用

- [ ] **Step 1: 搜索残留引用**

搜索以下内容确认无残留：
- `SetOnUpsert`
- `SetStopCh`
- `onUpsert`
- `CooldownSeconds`
- `cooldownSeconds`
- `summaryTicker`

Run: `rg "SetOnUpsert|SetStopCh|onUpsert|CooldownSeconds|cooldownSeconds|summaryTicker" --type go`

- [ ] **Step 2: 清理发现的残留引用**

如果有残留，逐一修复。

- [ ] **Step 3: 运行 make test 最终验证**

Run: `make test`
Expected: ALL PASS

- [ ] **Step 4: 运行 go vet**

Run: `go vet -tags "fts5" ./...`
Expected: 无警告

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: clean up removed onUpsert/stopCh/cooldown references"
```

---

### Task 5: 更新 spec 状态

- [ ] **Step 1: 在 spec 文件末尾追加实现状态**

在 `docs/superpowers/specs/2026-05-11-pipeline-unify-design.md` 末尾追加：

```markdown

## 七、实现状态

- [x] Task 1: UpsertSummaryWithVector DAO 方法
- [x] Task 2: Summarizer 重构 — embed 合并
- [x] Task 3: daemon pipelineTick + 删除 summaryTicker
- [x] Task 4: 清理残留代码
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-05-11-pipeline-unify-design.md
git commit -m "docs: update pipeline-unify spec with implementation status"
```

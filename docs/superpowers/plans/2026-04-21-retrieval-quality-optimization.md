# 检索质量优化 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 优化检索质量 — 分块参数缩小、Embedding 指令前缀、Rocchio PRF 查询扩展、MMR 去重排序

**Architecture:** 四个独立任务按优先级实施。P0（指令前缀 + 分块参数）需 rebuild 索引，P1（Rocchio + MMR）是纯查询侧改动。所有改动 TDD 驱动。

**Tech Stack:** Go, sqlite-vec, llama-go, got/convert

**Spec:** `docs/superpowers/specs/2026-04-21-retrieval-quality-optimization.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/embedding/llama.go` | Modify | EmbedQuery 加指令前缀 |
| `internal/embedding/mock.go` | Modify | EmbedQuery 加指令前缀（mock 一致） |
| `internal/embedding/llama_test.go` | Modify | 验证 EmbedQuery 含前缀 |
| `internal/chunker/markdown.go` | Modify | 分块参数 300、句级 overlap |
| `internal/chunker/markdown_test.go` | Modify | 更新测试用例适配新参数 |
| `internal/dao/chunks_vec.go` | Modify | 新增 GetEmbeddingsByChunkIds |
| `internal/dao/chunks_test.go` | Modify | 测试 GetEmbeddingsByChunkIds |
| `internal/service/rocchio.go` | Create | Rocchio embedding feedback |
| `internal/service/rocchio_test.go` | Create | Rocchio 测试 |
| `internal/service/mmr.go` | Create | MMR 去重排序 |
| `internal/service/mmr_test.go` | Create | MMR 测试 |
| `internal/service/searcher.go` | Modify | SearchVector 集成 Rocchio + MMR |
| `internal/daemon/routes.go` | Modify | handleQuery 集成 Rocchio + MMR |
| `docs/01.memory.md` | Modify | 同步更新 |

---

## Chunk 1: Embedding 指令前缀

### Task 1: LlamaProvider.EmbedQuery 加指令前缀

**Files:**
- Modify: `internal/embedding/llama.go:64-66`
- Modify: `internal/embedding/mock.go:31-33`
- Test: `internal/embedding/llama_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/embedding/llama_test.go` 末尾添加：

```go
func TestEmbedQueryPrefix(t *testing.T) {
	mock := NewMockProvider(32)

	vecDirect, _ := mock.Embed(context.Background(), "hello")
	vecQuery, _ := mock.EmbedQuery(context.Background(), "hello")

	if fmt.Sprintf("%x", vecDirect) == fmt.Sprintf("%x", vecQuery) {
		t.Fatal("EmbedQuery should produce different embedding than Embed (due to instruction prefix)")
	}
}
```

需 import `"fmt"`。

- [ ] **Step 2: 运行测试确认失败**

```bash
make test-verbose 2>&1 | grep TestEmbedQueryPrefix
```

Expected: FAIL（当前 EmbedQuery 和 Embed 结果一样）

- [ ] **Step 3: 实现 — LlamaProvider**

在 `internal/embedding/llama.go` 中添加常量并修改 `EmbedQuery`：

```go
const embedQueryPrefix = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

func (my *LlamaProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, embedQueryPrefix+query)
}
```

- [ ] **Step 4: 实现 — MockProvider**

在 `internal/embedding/mock.go` 中修改 `EmbedQuery`：

```go
const mockQueryPrefix = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

func (my *MockProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, mockQueryPrefix+query)
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
make test-verbose 2>&1 | grep -E "(TestEmbedQuery|FAIL)"
```

Expected: TestEmbedQueryPrefix PASS, 所有已有测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/embedding/llama.go internal/embedding/mock.go internal/embedding/llama_test.go
git commit -m "feat: add Qwen3-Embedding instruction prefix to EmbedQuery"
```

---

## Chunk 2: 分块参数调整

### Task 2: 分块参数 300 + 句级 overlap

**Files:**
- Modify: `internal/chunker/markdown.go:19-31`
- Modify: `internal/chunker/markdown_test.go`

- [ ] **Step 1: 修改分块参数**

在 `internal/chunker/markdown.go` 的 `NewMarkdownChunker` 中：

```go
func NewMarkdownChunker(chunkSize int) *MarkdownChunker {
	if chunkSize <= 0 {
		chunkSize = 300
	}
	overlapChars := chunkSize * 15 / 100
	if overlapChars < 30 {
		overlapChars = 30
	}
	return &MarkdownChunker{
		chunkSize:    chunkSize,
		hardMax:      chunkSize + chunkSize/2,
		overlapChars: overlapChars,
		windowChars:  chunkSize / 2,
		decayFactor:  0.7,
		minChunkSize: chunkSize / 4,
	}
}
```

比例关系：
- `hardMax` = chunkSize × 1.5（原 1200+300=1500 → 新 300+150=450）
- `overlapChars` = chunkSize × 15%（300 × 0.15 = 45）
- `windowChars` = chunkSize / 2（搜索断点的窗口）
- `minChunkSize` = chunkSize / 4（太小的碎片合并，300/4=75）

- [ ] **Step 2: 更新测试默认值**

在 `internal/chunker/markdown_test.go` 中：

```go
func TestNewMarkdownChunkerDefault(t *testing.T) {
	c := NewMarkdownChunker(0)
	if c.chunkSize != 300 {
		t.Fatalf("expected default chunkSize=300, got %d", c.chunkSize)
	}
	if c.overlapChars != 45 {
		t.Fatalf("expected default overlapChars=45, got %d", c.overlapChars)
	}
}
```

- [ ] **Step 3: 更新其它测试中的硬编码值**

所有测试中 `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`，hardMax 相关断言 `1500` → `450`：

- `TestChunkRespectsHardMax`: text `"a" × 2000` 不变（>450 就够分），断言 `1500` → `450`
- `TestChunkSplitByParagraph`: `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`
- `TestChunkHeadingBreakpoint`: `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`
- `TestChunkCodeFenceNotSplit`: `NewMarkdownChunker(500)` → `NewMarkdownChunker(200)`
- `TestChunkNoTinyFragments`: `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`，断言 `< 50` → `< 20`
- `TestChunkOverlapDoesNotExceedHardMax`: `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`，断言 `1500` → `450`
- `TestChunkStripsBase64Images`: `NewMarkdownChunker(800)` → `NewMarkdownChunker(300)`

- [ ] **Step 4: 运行测试确认通过**

```bash
make test-verbose 2>&1 | grep -E "(TestChunk|FAIL)"
```

Expected: 所有 chunker 测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/chunker/markdown.go internal/chunker/markdown_test.go
git commit -m "feat: reduce chunk size to 300 runes with proportional overlap"
```

---

## Chunk 3: Rocchio Embedding Feedback

### Task 3: dao — 按 chunk IDs 查询已有 embedding

**Files:**
- Modify: `internal/dao/chunks_vec.go`
- Modify: `internal/dao/chunks_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/dao/chunks_test.go` 末尾添加：

```go
func TestGetEmbeddingsByChunkIds(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = float32(i)
	}

	_, chunks := insertTestDoc(t, "test content")
	chunkId := chunks[0].ID

	err := dao.InsertVector(chunkId, vec)
	if err != nil {
		t.Fatal(err)
	}

	results, err := dao.GetEmbeddingsByChunkIds([]int64{chunkId})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ChunkID != chunkId {
		t.Fatalf("expected chunkId %d, got %d", chunkId, results[0].ChunkID)
	}
}

func TestGetEmbeddingsByChunkIdsEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	results, err := dao.GetEmbeddingsByChunkIds(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty input, got %d", len(results))
	}
}
```

注意：需在文件顶部 `import` 中检查是否已导入 `dao`，如未导入需参考同文件其它测试函数的 import 方式。`setupTestDB` 和 `insertTestDoc` 是已有的 test helper，参考同文件其它测试。

- [ ] **Step 2: 运行测试确认失败**

```bash
make test-verbose 2>&1 | grep TestGetEmbeddingsByChunkIds
```

Expected: FAIL（函数不存在）

- [ ] **Step 3: 实现**

在 `internal/dao/chunks_vec.go` 中添加：

```go
type ChunkEmbedding struct {
	ChunkID   int64
	Embedding []float32
}

func GetEmbeddingsByChunkIds(chunkIds []int64) ([]ChunkEmbedding, error) {
	if len(chunkIds) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(chunkIds))
	args := make([]any, len(chunkIds))
	for i, id := range chunkIds {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT chunk_id, embedding FROM chunks_vec WHERE chunk_id IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChunkEmbedding
	for rows.Next() {
		var r ChunkEmbedding
		var vecBlob []byte
		if err := rows.Scan(&r.ChunkID, &vecBlob); err != nil {
			return nil, err
		}
		r.Embedding = sqlite_vec.DeserializeFloat32(vecBlob, EmbeddingDim)
		results = append(results, r)
	}
	return results, rows.Err()
}
```

需在 import 中加 `"strings"`。

注意：`sqlite_vec.DeserializeFloat32` 的签名需确认。查看 `vendor/github.com/asg017/sqlite-vec-go-bindings/cgo/` 目录下的反序列化函数。如果该函数不存在或签名不同，改用 `math.Float32frombits` + `encoding/binary` 从 blob 解码。

- [ ] **Step 4: 运行测试确认通过**

```bash
make test-verbose 2>&1 | grep -E "(TestGetEmbeddings|FAIL)"
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/dao/chunks_vec.go internal/dao/chunks_test.go
git commit -m "feat: add GetEmbeddingsByChunkIds for Rocchio PRF"
```

### Task 4: Rocchio 算法

**Files:**
- Create: `internal/service/rocchio.go`
- Create: `internal/service/rocchio_test.go`

- [ ] **Step 1: 写 Rocchio 测试**

创建 `internal/service/rocchio_test.go`：

```go
package service

import (
	"math"
	"testing"
)

func TestRocchioBasic(t *testing.T) {
	queryVec := []float32{1.0, 0.0, 0.0, 0.0}
	docVecs := [][]float32{
		{0.0, 1.0, 0.0, 0.0},
		{0.0, 0.0, 1.0, 0.0},
	}

	result := Rocchio(queryVec, docVecs, 0.6, 0.4)

	if len(result) != 4 {
		t.Fatalf("expected dim 4, got %d", len(result))
	}

	if result[0] < 0.3 || result[0] > 0.7 {
		t.Fatalf("expected query component preserved, got %f", result[0])
	}
}

func TestRocchioNoDocs(t *testing.T) {
	queryVec := []float32{1.0, 0.0, 0.0, 0.0}
	result := Rocchio(queryVec, nil, 0.6, 0.4)

	if len(result) != 4 {
		t.Fatalf("expected dim 4, got %d", len(result))
	}
	for i, v := range result {
		if math.Abs(float64(v-queryVec[i])) > 1e-6 {
			t.Fatalf("expected original query when no docs, got %v", result)
		}
	}
}

func TestRocchioNormalized(t *testing.T) {
	queryVec := []float32{1.0, 0.0}
	docVecs := [][]float32{{0.0, 1.0}}
	result := Rocchio(queryVec, docVecs, 0.5, 0.5)

	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)

	if math.Abs(norm-1.0) > 1e-4 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestRocchioSingleDoc(t *testing.T) {
	queryVec := []float32{1.0, 0.0}
	docVecs := [][]float32{{0.0, 1.0}}
	result := Rocchio(queryVec, docVecs, 0.6, 0.4)

	if result[0] <= 0 || result[1] <= 0 {
		t.Fatalf("expected both components > 0, got %v", result)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
make test-verbose 2>&1 | grep TestRocchio
```

Expected: FAIL（函数不存在）

- [ ] **Step 3: 实现 Rocchio**

创建 `internal/service/rocchio.go`：

```go
package service

import "math"

func Rocchio(queryVec []float32, docVecs [][]float32, alpha, beta float64) []float32 {
	dim := len(queryVec)
	result := make([]float32, dim)

	for i, v := range queryVec {
		result[i] = float32(alpha) * v
	}

	if len(docVecs) > 0 {
		avg := make([]float32, dim)
		for _, doc := range docVecs {
			for i, v := range doc {
				avg[i] += v
			}
		}
		for i := range avg {
			avg[i] /= float32(len(docVecs))
			result[i] += float32(beta) * avg[i]
		}
	}

	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range result {
			result[i] /= float32(norm)
		}
	}

	return result
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
make test-verbose 2>&1 | grep -E "(TestRocchio|FAIL)"
```

Expected: 所有 Rocchio 测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/rocchio.go internal/service/rocchio_test.go
git commit -m "feat: add Rocchio embedding feedback for query expansion"
```

---

## Chunk 4: MMR 去重排序

### Task 5: MMR 实现

**Files:**
- Create: `internal/service/mmr.go`
- Create: `internal/service/mmr_test.go`

- [ ] **Step 1: 写 MMR 测试**

创建 `internal/service/mmr_test.go`：

```go
package service

import (
	"math"
	"testing"
)

func TestMMRBasic(t *testing.T) {
	query := []float32{1.0, 0.0}
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{0.9, 0.1}},
		{ID: 2, Embedding: []float32{0.85, 0.15}},
		{ID: 3, Embedding: []float32{0.1, 0.9}},
	}

	selected := SelectMMR(candidates, query, 0.7, 2)

	if len(selected) != 2 {
		t.Fatalf("expected 2 results, got %d", len(selected))
	}
	if selected[0].ID != 1 {
		t.Fatalf("expected first selected to be most relevant (ID=1), got ID=%d", selected[0].ID)
	}
	if selected[1].ID != 3 {
		t.Fatalf("expected second selected to be diverse (ID=3), got ID=%d", selected[1].ID)
	}
}

func TestMMREmptyCandidates(t *testing.T) {
	query := []float32{1.0, 0.0}
	selected := SelectMMR(nil, query, 0.7, 5)
	if len(selected) != 0 {
		t.Fatalf("expected 0 results, got %d", len(selected))
	}
}

func TestMMRFewerThanTopK(t *testing.T) {
	query := []float32{1.0, 0.0}
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{0.9, 0.1}},
	}
	selected := SelectMMR(candidates, query, 0.7, 5)
	if len(selected) != 1 {
		t.Fatalf("expected 1 result, got %d", len(selected))
	}
}

func TestMMRAllIdentical(t *testing.T) {
	query := []float32{1.0, 0.0}
	emb := []float32{0.8, 0.2}
	var norm float64
	for _, v := range emb {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	normed := make([]float32, len(emb))
	for i, v := range emb {
		normed[i] = v / float32(norm)
	}

	candidates := []MMRCandidate{
		{ID: 1, Embedding: normed},
		{ID: 2, Embedding: normed},
		{ID: 3, Embedding: normed},
	}

	selected := SelectMMR(candidates, query, 0.7, 3)

	ids := make(map[int64]bool)
	for _, s := range selected {
		if ids[s.ID] {
			t.Fatalf("duplicate ID %d in selection", s.ID)
		}
		ids[s.ID] = true
	}
}

func TestMMRLambdaOne(t *testing.T) {
	query := []float32{1.0, 0.0}
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{0.5, 0.5}},
		{ID: 2, Embedding: []float32{0.9, 0.1}},
		{ID: 3, Embedding: []float32{0.1, 0.9}},
	}

	selected := SelectMMR(candidates, query, 1.0, 3)

	if selected[0].ID != 2 {
		t.Fatalf("lambda=1.0 should be pure relevance, expected ID=2 first, got ID=%d", selected[0].ID)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
make test-verbose 2>&1 | grep TestMMR
```

Expected: FAIL

- [ ] **Step 3: 实现 MMR**

创建 `internal/service/mmr.go`：

```go
package service

type MMRCandidate struct {
	ID        int64
	Embedding []float32
}

func SelectMMR(candidates []MMRCandidate, queryVec []float32, lambda float64, topK int) []MMRCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if topK > len(candidates) {
		topK = len(candidates)
	}

	var selected []MMRCandidate
	remaining := make([]MMRCandidate, len(candidates))
	copy(remaining, candidates)

	for len(selected) < topK {
		bestIdx := 0
		bestScore := -1e9

		for i, cand := range remaining {
			relevance := cosineSimilarity(cand.Embedding, queryVec)

			var maxSim float64
			for _, s := range selected {
				sim := cosineSimilarity(cand.Embedding, s.Embedding)
				if sim > maxSim {
					maxSim = sim
				}
			}

			score := lambda*relevance - (1-lambda)*maxSim
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
```

需 import `"math"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
make test-verbose 2>&1 | grep -E "(TestMMR|FAIL)"
```

Expected: 所有 MMR 测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/mmr.go internal/service/mmr_test.go
git commit -m "feat: add MMR (Maximal Marginal Relevance) for result diversity"
```

---

## Chunk 5: 集成到搜索流程

### Task 6: Searcher 集成 Rocchio + MMR

**Files:**
- Modify: `internal/service/searcher.go:56-63`
- Modify: `internal/daemon/routes.go:82-129`

- [ ] **Step 1: 写 SearcherWithPRF 测试**

在 `internal/service/rocchio_test.go` 末尾追加集成测试思路描述（非完整集成测试，因需要 DB）：

集成测试在 `internal/daemon/daemon_test.go` 中通过 httptest 调 `/query` 验证，此处只验证单元函数正确。

Searcher 的改动主要是流程编排，通过手动 `lmd search` + `lmd hyde` 做端到端验证。

- [ ] **Step 2: 修改 Searcher.SearchVector 支持 Rocchio**

在 `internal/service/searcher.go` 中修改 `SearchVector`：

```go
func (my *Searcher) SearchVector(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchVector: query=%q collection=%s limit=%d", query, collection, limit)
	queryVec, err := provider.EmbedQuery(context.Background(), query)
	if err != nil {
		return nil, err
	}
	return my.SearchVectorByEmbedding(queryVec, collection, limit), nil
}

func (my *Searcher) SearchVectorWithPRF(provider embedding.EmbeddingProvider, query, collection string, limit int, ftsHits []formatter.SearchHit) ([]formatter.SearchHit, error) {
	logo.Info("SearchVectorWithPRF: query=%q collection=%s ftsHits=%d", query, collection, len(ftsHits))
	queryVec, err := provider.EmbedQuery(context.Background(), query)
	if err != nil {
		return nil, err
	}

	if len(ftsHits) >= 3 {
		var chunkIds []int64
		for i := 0; i < 3 && i < len(ftsHits); i++ {
			chunkIds = append(chunkIds, ftsHits[i].ChunkId)
		}

		embeddings, err := dao.GetEmbeddingsByChunkIds(chunkIds)
		if err == nil && len(embeddings) > 0 {
			var docVecs [][]float32
			for _, e := range embeddings {
				docVecs = append(docVecs, e.Embedding)
			}
			queryVec = Rocchio(queryVec, docVecs, 0.6, 0.4)
			logo.Info("SearchVectorWithPRF: Rocchio applied with %d feedback docs", len(docVecs))
		}
	}

	return my.SearchVectorByEmbedding(queryVec, collection, limit), nil
}
```

注意：需在 import 中加 `"github.com/lixianmin/lmd/internal/dao"`。

- [ ] **Step 3: 修改 handleQuery 集成 Rocchio + MMR**

在 `internal/daemon/routes.go` 的 `handleQuery` 中，将 `my.searcher.SearchVector` 替换为 `my.searcher.SearchVectorWithPRF`：

```go
func (my *Daemon) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit*3, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	vecHits, err := my.searcher.SearchVectorWithPRF(my.provider, req.Query, req.Collection, req.Limit*3, lexHits)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results := service.FuseResults(lexHits, vecHits)

	if req.MinScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range results {
			if h.Score >= req.MinScore {
				filtered = append(filtered, h)
			}
		}
		results = filtered
	}

	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}

	logo.Info("handleQuery: query=%q collection=%s lex=%d vec=%d results=%d",
		req.Query, req.Collection, len(lexHits), len(vecHits), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}
```

- [ ] **Step 4: 运行全部测试**

```bash
make test 2>&1 | grep -E "(^ok|^FAIL)"
```

Expected: 所有包 PASS

- [ ] **Step 5: 端到端验证**

```bash
make both
sleep 3
lmd search "docker命令"
lmd hyde "docker命令"
```

Expected: 搜索结果正常返回，无报错。

- [ ] **Step 6: 提交**

```bash
git add internal/service/searcher.go internal/daemon/routes.go
git commit -m "feat: integrate Rocchio PRF into hybrid search pipeline"
```

---

## Chunk 6: 收尾

### Task 7: 更新 memory.md + rebuild 索引

**Files:**
- Modify: `docs/01.memory.md`

- [ ] **Step 1: 确认 memory.md 已在 Task 1 前更新**

检查 `docs/01.memory.md` 的 Key Technical Decisions 部分是否已包含新参数。如有遗漏补充。

- [ ] **Step 2: rebuild 索引验证完整流程**

```bash
lmd rebuild
```

Expected: 旧索引清除，重新分块（300 字/chunk）和 embedding（含指令前缀）。

- [ ] **Step 3: 最终全量测试**

```bash
make test 2>&1 | grep -E "(^ok|^FAIL)"
make vet
```

Expected: 全部通过。

- [ ] **Step 4: 提交收尾**

```bash
git add docs/01.memory.md
git commit -m "docs: update memory.md with new retrieval parameters"
```

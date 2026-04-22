# Memory Query 实现计划

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Memory Search 改为混合检索（BM25 + Vector + RRF），统一命名为 Query，加入半衰期硬遗忘

**Architecture:** 泛化 RRF 算法解耦 `SearchHit`，新建 `memories_vec` 表支持向量检索，Memory Query 复用文档搜索 pipeline，最后全链路重命名 search→query

**Tech Stack:** Go, sqlite-vec, got/convert

**Spec:** `docs/superpowers/specs/2026-04-22-memory-query-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/service/rrf.go` | Modify | 泛化 RRF，用 `RankedItem` 中间层替代 `SearchHit` |
| `internal/service/fusion.go` | Modify | `FuseResults` 使用泛化 RRF |
| `internal/service/fusion_test.go` | Modify | 适配泛化 RRF |
| `internal/dao/db_init.go` | Modify | 新增 `memories_vec` 表 |
| `internal/dao/memory.go` | Modify | 新增向量搜索，`InsertMemory`/`UpdateMemoryEmbedding` 同步 vec 表，删除 `SearchMemoryFTSByType` |
| `internal/dao/memory_test.go` | Modify | 更新测试 |
| `internal/dao/stats.go` | Modify | 新增 memory embedding 计数（基于 `memories_vec`） |
| `internal/service/memory.go` | Modify | `Search`→`Query`，混合检索 + 半衰期硬遗忘 |
| `internal/service/memory_test.go` | Modify | 更新测试 |
| `internal/daemon/routes.go` | Modify | `handleMemorySearch`→`handleMemoryQuery`，路由路径、请求参数 |
| `internal/daemon/client.go` | Modify | `MemorySearch`→`MemoryQuery` |
| `internal/daemon/server.go` | Modify | 路由 `/memory/search`→`/memory/query` |
| `internal/daemon/daemon.go` | Modify | `embedMemories` 写入 `memories_vec` |
| `internal/cli/memory.go` | Modify | CLI `memory search`→`memory query`，去掉 `--type` flag |
| `internal/mcp/server.go` | Modify | `memory_search`→`memory_query` |
| `internal/mcp/server_test.go` | Modify | 更新 tool name 测试 |

---

## Chunk 1: RRF 泛化

### Task 1: 新增 `RankedItem` 并泛化 `ReciprocalRankFusion`

**Files:**
- Modify: `internal/service/rrf.go`
- Test: `internal/service/fusion_test.go`

- [ ] **Step 1: 写失败测试 — 验证泛化 RRF 可用于非 SearchHit 类型**

在 `internal/service/fusion_test.go` 末尾新增：

```go
func TestRRFWithRankedItems(t *testing.T) {
	lists := [][]RankedItem{
		{{Key: 10, Score: 0.8}, {Key: 20, Score: 0.6}},
		{{Key: 20, Score: 0.9}, {Key: 30, Score: 0.5}},
	}
	results := ReciprocalRankFusionGeneric(lists, DefaultRRFParams())
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	if results[0].Key != 20 {
		t.Fatalf("expected key 20 first (rank 0 in vec, rank 1 in lex), got %d", results[0].Key)
	}
	keys := map[int64]bool{}
	for _, r := range results {
		keys[r.Key] = true
	}
	for _, k := range []int64{10, 20, 30} {
		if !keys[k] {
			t.Fatalf("expected key %d in results", k)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestRRFWithRankedItems ./internal/service/`
Expected: FAIL — `RankedItem` and `ReciprocalRankFusionGeneric` undefined

- [ ] **Step 3: 在 `rrf.go` 中新增 `RankedItem` 和 `ReciprocalRankFusionGeneric`**

在 `internal/service/rrf.go` 中新增：

```go
type RankedItem struct {
	Key   int64
	Score float64
}

func ReciprocalRankFusionGeneric(lists [][]RankedItem, params RRFParams) []RankedItem {
	type entry struct {
		item     RankedItem
		score    float64
		bestRank int
	}

	scores := make(map[int64]*entry)

	for i, list := range lists {
		if list == nil {
			continue
		}
		var weight float64
		if i < len(params.Weights) {
			weight = params.Weights[i]
		} else if i < 2 {
			weight = 2.0
		} else {
			weight = 1.0
		}

		for r, item := range list {
			contribution := weight / (params.K + float64(r) + 1)
			if existing, ok := scores[item.Key]; ok {
				existing.score += contribution
				if r < existing.bestRank {
					existing.bestRank = r
				}
			} else {
				scores[item.Key] = &entry{
					item:     item,
					score:    contribution,
					bestRank: r,
				}
			}
		}
	}

	for _, e := range scores {
		if e.bestRank == 0 {
			e.score += params.TopRankBonus1
		} else if e.bestRank <= 2 {
			e.score += params.TopRankBonus2
		}
	}

	results := make([]RankedItem, 0, len(scores))
	for _, e := range scores {
		results = append(results, e.item)
	}

	sort.Slice(results, func(i, j int) bool {
		ki := results[i].Key
		kj := results[j].Key
		if scores[ki].score != scores[kj].score {
			return scores[ki].score > scores[kj].score
		}
		return ki < kj
	})

	maxScore := 0.0
	for _, e := range scores {
		if e.score > maxScore {
			maxScore = e.score
		}
	}
	for i := range results {
		if maxScore > 0 {
			results[i].Score = scores[results[i].Key].score / maxScore
		}
	}

	return results
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestRRFWithRankedItems ./internal/service/`
Expected: PASS

### Task 2: 重构 `ReciprocalRankFusion` 使用 `ReciprocalRankFusionGeneric`

**Files:**
- Modify: `internal/service/rrf.go`
- Modify: `internal/service/fusion.go`
- Test: `internal/service/fusion_test.go`

- [ ] **Step 1: 改写 `ReciprocalRankFusion` 为 `ReciprocalRankFusionGeneric` 的 wrapper**

将 `rrf.go` 中的 `ReciprocalRankFusion` 改为：

```go
func ReciprocalRankFusion(lists [][]formatter.SearchHit, params RRFParams) []formatter.SearchHit {
	var genericLists [][]RankedItem
	for _, list := range lists {
		var items []RankedItem
		for _, h := range list {
			items = append(items, RankedItem{Key: h.ChunkId})
		}
		genericLists = append(genericLists, items)
	}

	ranked := ReciprocalRankFusionGeneric(genericLists, params)

	scoreMap := make(map[int64]float64, len(ranked))
	for _, r := range ranked {
		scoreMap[r.Key] = r.Score
	}

	hitMap := make(map[int64]formatter.SearchHit)
	for _, list := range lists {
		for _, h := range list {
			if existing, ok := hitMap[h.ChunkId]; !ok || existing.Snippet == "" && h.Snippet != "" {
				hitMap[h.ChunkId] = h
			}
		}
	}

	results := make([]formatter.SearchHit, 0, len(ranked))
	for _, r := range ranked {
		hit := hitMap[r.Key]
		hit.Score = r.Score
		results = append(results, hit)
	}

	return results
}
```

- [ ] **Step 2: 运行全部 fusion 测试确认无回归**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestFuseResults ./internal/service/`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/rrf.go internal/service/fusion_test.go
git commit -m "refactor: generalize RRF with RankedItem to support non-SearchHit types"
```

---

## Chunk 2: `memories_vec` 表 + DAO 层

### Task 3: 在 `db_init.go` 新增 `memories_vec` 表

**Files:**
- Modify: `internal/dao/db_init.go`

- [ ] **Step 1: 在 `createTables()` 中添加 `memories_vec` 表**

在 `chunks_vec` 表定义之后、`memories` 表定义之前，新增：

```go
`CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
    memory_id INTEGER PRIMARY KEY,
    embedding float[1024] distance_metric=cosine
)`,
```

注意：`memories_vec` 放在 `memories` 表之前不需要外键（vec0 不支持外键），但逻辑上跟 `chunks_vec` 一样是对 memories 表的向量索引。

- [ ] **Step 2: Commit**

```bash
git add internal/dao/db_init.go
git commit -m "feat: add memories_vec table for vector similarity search"
```

### Task 4: 修改 `InsertMemory` 和 `UpdateMemoryEmbedding` 同步 `memories_vec`

**Files:**
- Modify: `internal/dao/memory.go`
- Modify: `internal/dao/memory_test.go`

- [ ] **Step 1: 写失败测试 — 验证插入 memory 后 embedding 存在 memories_vec**

在 `internal/dao/memory_test.go` 中新增：

```go
func TestInsertMemorySyncsVec(t *testing.T) {
	initTestDB(t)

	id, _ := InsertMemory("test vec sync", "fact")

	count := 0
	dao.DB.QueryRow("SELECT COUNT(*) FROM memories_vec WHERE memory_id=?", id).Scan(&count)
	if count != 0 {
		t.Fatal("new memory should not have embedding yet")
	}
}

func TestUpdateMemoryEmbeddingSyncsVec(t *testing.T) {
	initTestDB(t)

	id, _ := InsertMemory("test vec sync", "fact")

	vec := padVector([]float32{0.1, 0.2, 0.3})
	serialized, _ := sqlite_vec.SerializeFloat32(vec)
	UpdateMemoryEmbedding(id, serialized)

	count := 0
	dao.DB.QueryRow("SELECT COUNT(*) FROM memories_vec WHERE memory_id=?", id).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 row in memories_vec, got %d", count)
	}
}
```

注意：`padVector` 已在 `chunks_vec.go` 中定义（同包可直接调用）。

- [ ] **Step 2: 运行测试验证失败**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run "TestInsertMemorySyncsVec|TestUpdateMemoryEmbeddingSyncsVec" ./internal/dao/`
Expected: FAIL — memories_vec doesn't sync yet

- [ ] **Step 3: 修改 `UpdateMemoryEmbedding` 同步写入 `memories_vec`**

```go
func UpdateMemoryEmbedding(id int64, vec []byte) error {
	return withTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE memories SET embedding=? WHERE id=?", vec, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT OR REPLACE INTO memories_vec(memory_id, embedding) VALUES (?, ?)", id, vec)
		return err
	})
}
```

注意：`memories` 表的 `embedding` 列保留（向后兼容），但不再作为查询入口。新查询全部走 `memories_vec`。

- [ ] **Step 4: 运行测试验证通过**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run "TestInsertMemorySyncsVec|TestUpdateMemoryEmbeddingSyncsVec" ./internal/dao/`
Expected: PASS

- [ ] **Step 5: 修改 `GetUnembeddedMemoryCount` 基于 `memories_vec` 判断**

```go
func GetUnembeddedMemoryCount() int {
	var count int
	DB.db.QueryRow(`
		SELECT COUNT(*) FROM memories m
		LEFT JOIN memories_vec v ON m.id = v.memory_id
		WHERE v.memory_id IS NULL
	`).Scan(&count)
	return count
}
```

同理修改 `GetUnembeddedMemories`：

```go
func GetUnembeddedMemories(limit int) ([]MemoryRecord, error) {
	rows, err := withQuery(`
		SELECT m.id, m.content, m.type, m.created_at
		FROM memories m
		LEFT JOIN memories_vec v ON m.id = v.memory_id
		WHERE v.memory_id IS NULL
		LIMIT ?
	`, limit)
	// ... 后续不变
}
```

- [ ] **Step 6: 运行全部 memory DAO 测试**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestMemory ./internal/dao/`
Expected: ALL PASS（旧的 `TestUpdateMemoryEmbedding` 验证 `rec.Embedding` 仍然通过，因为我们保留了 BLOB 列的写入）

- [ ] **Step 7: Commit**

```bash
git add internal/dao/memory.go internal/dao/memory_test.go
git commit -m "feat: sync memories_vec on embedding update, use vec table for unembedded check"
```

### Task 5: 新增 `SearchMemoryVector` 向量搜索

**Files:**
- Modify: `internal/dao/memory.go`
- Modify: `internal/dao/memory_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestSearchMemoryVector(t *testing.T) {
	initTestDB(t)

	InsertMemory("user likes coffee", "relation")
	InsertMemory("python is great", "fact")

	results, err := GetUnembeddedMemories(10)
	if err != nil {
		t.Fatal(err)
	}

	fakeVec := make([]float32, EmbeddingDim)
	for i := range fakeVec {
		fakeVec[i] = 0.01
	}
	serialized, _ := sqlite_vec.SerializeFloat32(padVector(fakeVec))
	UpdateMemoryEmbedding(results[0].ID, serialized)

	queryVec := make([]float32, EmbeddingDim)
	for i := range queryVec {
		queryVec[i] = 0.01
	}

	vecResults, err := SearchMemoryVector(queryVec, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecResults) == 0 {
		t.Fatal("expected at least 1 vector result")
	}
	if vecResults[0].ID != results[0].ID {
		t.Fatalf("expected memory %d, got %d", results[0].ID, vecResults[0].ID)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestSearchMemoryVector ./internal/dao/`
Expected: FAIL — `SearchMemoryVector` undefined

- [ ] **Step 3: 实现 `SearchMemoryVector`**

在 `internal/dao/memory.go` 中新增：

```go
func SearchMemoryVector(query []float32, limit int) ([]MemoryRecord, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT v.memory_id, v.distance
		FROM memories_vec v
		WHERE v.embedding MATCH ?
		ORDER BY v.distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var id int64
		var distance float64
		if err := rows.Scan(&id, &distance); err != nil {
			return nil, err
		}
		score := 1.0 - distance
		results = append(results, MemoryRecord{ID: id, Score: score})
	}

	for i := range results {
		row := withQueryRow("SELECT content, type, created_at FROM memories WHERE id=?", results[i].ID)
		if err := row.Scan(&results[i].Content, &results[i].Type, &results[i].CreatedAt); err != nil {
			continue
		}
	}

	return results, rows.Err()
}
```

注意：需要 import `sqlite_vec` 包。`padVector` 在同包的 `chunks_vec.go` 中已定义。

- [ ] **Step 4: 运行测试验证通过**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestSearchMemoryVector ./internal/dao/`
Expected: PASS

- [ ] **Step 5: 删除 `SearchMemoryFTSByType`，简化 `SearchMemoryFTS`**

删除 `SearchMemoryFTSByType` 函数和 `searchMemoryFTSFiltered`，`SearchMemoryFTS` 直接实现：

```go
func SearchMemoryFTS(tokenizedQuery string, limit int) ([]MemoryRecord, error) {
	query := `
		SELECT m.id, m.content, m.type, abs(f.rank) as raw_score, m.created_at
		FROM memories_fts f
		JOIN memories m ON m.id = f.rowid
		WHERE f.content MATCH ?
		ORDER BY rank LIMIT ?
	`
	rows, err := withQuery(query, tokenizedQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var rec MemoryRecord
		var rawScore float64
		if err := rows.Scan(&rec.ID, &rec.Content, &rec.Type, &rawScore, &rec.CreatedAt); err != nil {
			return nil, err
		}
		abs := math.Abs(rawScore)
		rec.Score = abs / (1.0 + abs)
		results = append(results, rec)
	}
	return results, rows.Err()
}
```

- [ ] **Step 6: 删除 `TestSearchMemoryFTSByType` 测试**

- [ ] **Step 7: 运行全部 memory DAO 测试**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 ./internal/dao/`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/dao/memory.go internal/dao/memory_test.go
git commit -m "feat: add SearchMemoryVector, remove FTS type filtering"
```

---

## Chunk 3: MemoryService.Query 混合检索

### Task 6: 重写 `MemoryService.Query` 为混合检索 + 半衰期硬遗忘

**Files:**
- Modify: `internal/service/memory.go`
- Modify: `internal/service/memory_test.go`

- [ ] **Step 1: 写失败测试 — 验证 Query 返回混合检索结果**

```go
func TestMemoryQueryNoDecay(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	svc.Add("user prefers dark mode", "fact")
	svc.Add("light theme is default", "fact")

	results, err := svc.Query("dark", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Score <= 0 {
		t.Fatalf("expected positive score, got %f", results[0].Score)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestMemoryQueryNoDecay ./internal/service/`
Expected: FAIL — `Query` undefined

- [ ] **Step 3: 实现 `Query` 方法**

将 `memory.go` 中的 `Search` 方法替换为 `Query`：

```go
const forgetThreshold = 0.05

func (my *MemoryService) Query(query string, limit int) ([]MemorySearchResult, error) {
	ftsQuery := query
	if my.tokenizer != nil {
		tokenized := my.tokenizer.TokenizeToString(query)
		if tokenized != "" {
			ftsQuery = tokenized
		}
	}

	ftsRecords, err := dao.SearchMemoryFTS(ftsQuery, limit*3)
	if err != nil {
		return nil, err
	}

	var ftsItems []RankedItem
	for _, r := range ftsRecords {
		ftsItems = append(ftsItems, RankedItem{Key: r.ID})
	}

	var vecRecords []dao.MemoryRecord
	if my.provider != nil {
		vec, embedErr := my.provider.Embed(context.Background(), query)
		if embedErr == nil {
			vecRecords, _ = dao.SearchMemoryVector(vec, limit*3)
		}
	}

	var vecItems []RankedItem
	for _, r := range vecRecords {
		vecItems = append(vecItems, RankedItem{Key: r.ID})
	}

	ranked := ReciprocalRankFusionGeneric([][]RankedItem{ftsItems, vecItems}, DefaultRRFParams())

	scoreMap := make(map[int64]float64, len(ranked))
	for _, r := range ranked {
		scoreMap[r.Key] = r.Score
	}

	recordMap := make(map[int64]dao.MemoryRecord)
	for _, r := range ftsRecords {
		recordMap[r.ID] = r
	}
	for _, r := range vecRecords {
		if _, ok := recordMap[r.ID]; !ok {
			recordMap[r.ID] = r
		}
	}

	now := time.Now()
	var results []MemorySearchResult
	for _, r := range ranked {
		rec := recordMap[r.Key]
		ageDays := now.Sub(rec.CreatedAt).Hours() / 24
		decay := decayFactor(rec.Type, ageDays)
		finalScore := r.Score * decay

		if finalScore < forgetThreshold && decay < 1.0 {
			continue
		}

		results = append(results, MemorySearchResult{
			ID:        rec.ID,
			Content:   rec.Content,
			Type:      rec.Type,
			Score:     finalScore,
			RawScore:  r.Score,
			CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
```

注意：`MemoryService` 需要新增 `provider` 字段。修改 `NewMemoryService`：

```go
type MemoryService struct {
	tokenizer tokenizer.Tokenizer
	provider  embedding.EmbeddingProvider
}

func NewMemoryService(tok tokenizer.Tokenizer, prov embedding.EmbeddingProvider) *MemoryService {
	return &MemoryService{tokenizer: tok, provider: prov}
}
```

这会影响 `daemon.go` 中的初始化调用，需要同步更新。但本 task 先只改 service 层，daemon 层在后续 task 处理。测试中传 `nil` 即可。

需要新增 import：
```go
import (
	"context"
	"time"
	// ... 其他已有
	"github.com/lixianmin/lmd/internal/embedding"
)
```

- [ ] **Step 4: 运行测试验证通过**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -run TestMemoryQueryNoDecay ./internal/service/`
Expected: PASS

- [ ] **Step 5: 更新其余 memory service 测试**

将所有 `svc.Search(...)` 调用改为 `svc.Query(...)`，去掉 `memType` 参数。更新 `NewMemoryService(nil)` 为 `NewMemoryService(nil, nil)`。

具体变更：
- `TestMemorySearchNoDecay` → `TestMemoryQueryNoDecay`（已写）
- `TestMemorySearchFactNoDecay` → 改用 `svc.Query`
- `TestMemorySearchEpisodeDecay` → 改用 `svc.Query`
- `TestMemorySearchRelationDecay` → 改用 `svc.Query`
- `TestMemorySearchFilterByType` → **删除**（不再按 type 过滤）
- `TestDecayFactor` → 保留不变（纯函数测试）
- `TestMemoryAdd` → `NewMemoryService(nil)` 改为 `NewMemoryService(nil, nil)`
- `TestMemoryAddInvalidType` → 同上

- [ ] **Step 6: 新增硬遗忘测试**

```go
func TestMemoryQueryHardForget(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil, nil)

	id, _ := svc.Add("old episode", "episode")
	oldTime := time.Now().Add(-100 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	dao.WithExec("UPDATE memories SET created_at=? WHERE id=?", oldTime, id)

	results, err := svc.Query("old episode", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == id {
			t.Fatal("100-day-old episode should be forgotten (hard forget)")
		}
	}
}
```

- [ ] **Step 7: 运行全部 memory service 测试**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 ./internal/service/`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/service/memory.go internal/service/memory_test.go
git commit -m "feat: MemoryService.Query with hybrid search (BM25+Vector+RRF) and hard forget"
```

---

## Chunk 4: 全链路重命名 search → query

### Task 7: 更新 daemon 层（routes + client + server + daemon）

**Files:**
- Modify: `internal/daemon/routes.go`
- Modify: `internal/daemon/client.go`
- Modify: `internal/daemon/server.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: `routes.go` — 重命名 handler 和更新请求参数**

- `handleMemorySearch` → `handleMemoryQuery`
- 请求结构体去掉 `Type` 字段
- 调用 `my.memSvc.Query(req.Query, req.Limit)`
- MCP `memory_query` case 也去掉 `Type` 字段
- `handleMemoryAdd` 中的请求不变（仍传 type）

- [ ] **Step 2: `client.go` — `MemorySearch` → `MemoryQuery`**

```go
func (c *Client) MemoryQuery(query string, limit int) ([]byte, error) {
	return c.Post("/memory/query", map[string]interface{}{
		"query": query,
		"limit": limit,
	})
}
```

- [ ] **Step 3: `server.go` — 路由路径更新**

`{"POST", "/memory/search", (*Daemon).handleMemorySearch}` → `{"POST", "/memory/query", (*Daemon).handleMemoryQuery}`

- [ ] **Step 4: `daemon.go` — 更新 `embedMemories` 和 `MemoryService` 初始化**

`embedMemories` 中 `UpdateMemoryEmbedding` 已在 Task 4 改为同步写入 `memories_vec`，此处无需额外改动。

`MemoryService` 初始化改为：
```go
my.memSvc = service.NewMemoryService(my.tokenizer, my.provider)
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/routes.go internal/daemon/client.go internal/daemon/server.go internal/daemon/daemon.go
git commit -m "refactor: rename memory search→query in daemon layer"
```

### Task 8: 更新 CLI 和 MCP

**Files:**
- Modify: `internal/cli/memory.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/server_test.go`

- [ ] **Step 1: `cli/memory.go` — 重命名命令和去掉 `--type` flag**

- `memorySearchCmd` → `memoryQueryCmd`
- `Use: "search <query>"` → `Use: "query <query>"`
- 去掉 `--type` flag
- `client.MemorySearch` → `client.MemoryQuery`
- `memorySearchCmd` → `memoryQueryCmd` 在 `init()` 注册

- [ ] **Step 2: `mcp/server.go` — tool name 更新**

`{Name: "memory_search", ...}` → `{Name: "memory_query", ...}`

- [ ] **Step 3: `mcp/server_test.go` — 更新 tool name**

`"memory_search"` → `"memory_query"`

- [ ] **Step 4: Commit**

```bash
git add internal/cli/memory.go internal/mcp/server.go internal/mcp/server_test.go
git commit -m "refactor: rename memory search→query in CLI and MCP"
```

---

## Chunk 5: 清理 + 验证

### Task 9: 全量测试 + 清理

**Files:**
- Modify: `docs/01.memory.md`

- [ ] **Step 1: 运行全量测试**

Run: `LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go CGO_LDFLAGS="-lggml-metal -lggml-blas" go test -tags fts5 -count=1 -v ./internal/...`
Expected: ALL PASS

- [ ] **Step 2: 更新 `docs/01.memory.md`**

记录 Memory Query 混合检索、半衰期硬遗忘、`memories_vec` 表等决策。

- [ ] **Step 3: Final commit**

```bash
git add docs/01.memory.md
git commit -m "docs: update memory.md with hybrid query and hard-forget design"
```

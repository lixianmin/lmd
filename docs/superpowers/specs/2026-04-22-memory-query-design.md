# Memory Query: 混合检索 + 半衰期遗忘

## 背景

当前 Memory 只有 FTS 关键词搜索，且读时按 type 过滤。需要改为：

1. 混合检索：BM25 + Vector + RRF（与文档搜索 pipeline 一致）
2. 半衰期硬遗忘：衰减后 score < 0.05 的记忆直接过滤
3. 读时不按 type 过滤

同时，所有 "memory search" 命名统一改为 "memory query"。

## 命名变更

| 位置 | 旧 | 新 |
|------|-----|-----|
| `MemoryService.Search()` | `Search` | `Query` |
| CLI 命令 | `lmd memory search` | `lmd memory query` |
| HTTP 路由 | `POST /memory/search` | `POST /memory/query` |
| MCP tool | `memory_search` | `memory_query` |
| `handleMemorySearch` | `handleMemorySearch` | `handleMemoryQuery` |
| `client.MemorySearch()` | `MemorySearch` | `MemoryQuery` |

## 架构：复用混合搜索 pipeline

### 现状分析

文档搜索和 Memory 搜索的 pipeline 完全相同：

```
BM25 (FTS5) + Vector (sqlite-vec) → RRF fusion → 结果
```

差异仅在底层表和数据模型：

| | 文档搜索 | Memory Query |
|--|---------|-------------|
| FTS 表 | `chunks_fts` | `memories_fts` |
| Vector 存储 | `chunks_vec` | `memories` 表的 `embedding` 列 |
| 去重 key | `ChunkId` | `Memory ID` |
| 结果类型 | `formatter.SearchHit` | `MemorySearchResult` |
| 后处理 | 无 | 半衰期衰减 + 硬遗忘过滤 |
| Collection 过滤 | 有 | 无 |

### RRF 泛化

当前 `ReciprocalRankFusion()` 硬编码使用 `formatter.SearchHit` 和 `ChunkId` 做 key。需要抽象为通用接口：

```go
type Rankable interface {
    RankKey() int64
}
```

`SearchHit` 和 `MemorySearchResult` 都实现 `RankKey()`。RRF 泛化为：

```go
func ReciprocalRankFusion[T Rankable](lists [][]T, params RRFParams) []T
```

或者更简单地，用一个 `RankedItem` 结构做中间层：

```go
type RankedItem struct {
    Key   int64
    Score float64
}

func RRFByKeys(lists [][]RankedItem, params RRFParams) []RankedItem
```

各调用方将结果转为 `RankedItem` 后调用，再映射回原始类型。

### Memory Query 流程

```
1. tokenizer 分词 → BM25 搜索 memories_fts → []MemorySearchResult (scored)
2. EmbedQuery → 向量搜索 memories → []MemorySearchResult (scored)
3. 转为 []RankedItem → RRFByKeys → 融合排序
4. 映射回 []MemorySearchResult
5. 应用 decayFactor（按 type 计算半衰期）
6. 过滤 score < 0.05 的结果（硬遗忘）
```

## 半衰期参数（不变）

| Type | 半衰期 | 遗忘阈值 | 遗忘时间（约） |
|------|--------|---------|-------------|
| fact | 永不衰减 | — | 永不遗忘 |
| episode | 15 天 | 0.05 | ~65 天 |
| relation | 180 天 | 0.05 | ~780 天 |

## DAO 层变更

### 新增

| 函数 | 说明 |
|------|------|
| `SearchMemoryVector(vec []float32, limit int) ([]MemoryRecord, error)` | 按 embedding 向量搜索 memories |

实现：遍历 `memories` 表中 `embedding IS NOT NULL` 的行，计算 cosine similarity，取 top-N。

注意：memories 没有独立的 `memories_vec` 表（embedding 直接存在 memories 表的 BLOB 列中），所以不能复用 `chunks_vec` 的 `vec0` 查询。需要用 Go 代码计算 cosine similarity 或创建 `memories_vec` 表。

**推荐**：创建 `memories_vec` 虚拟表，与 `chunks_vec` 结构一致：

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
    memory_id INTEGER PRIMARY KEY,
    embedding float[1024] distance_metric=cosine
)
```

这样可以直接复用 `dao.QueryVectors` 的查询模式。

### 删除

| 函数 | 原因 |
|------|------|
| `SearchMemoryFTSByType` | 读时不按 type 过滤 |
| `SearchMemoryFTS` | 改为 `SearchMemoryFTS` 保留，但去掉 type 过滤参数 |

### 保留

| 函数 | 说明 |
|------|------|
| `InsertMemory` | 写时仍按 type 写入 |
| `GetMemoryByID` | 不变 |
| `UpdateMemoryEmbedding` | 不变（同时更新 `memories_vec`） |
| `GetUnembeddedMemories` | 不变 |
| `GetUnembeddedMemoryCount` | 不变 |

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/dao/db_init.go` | 新增 `memories_vec` 表 |
| `internal/dao/memory.go` | 新增 `SearchMemoryVector`，删除 `SearchMemoryFTSByType`，修改 `InsertMemory`/`UpdateMemoryEmbedding` 同步 vec 表 |
| `internal/dao/memory_test.go` | 更新测试 |
| `internal/service/rrf.go` | 泛化 RRF，解耦 `SearchHit` |
| `internal/service/rrf_test.go` | 更新测试 |
| `internal/service/memory.go` | `Search` → `Query`，实现混合检索 + 半衰期过滤 |
| `internal/service/memory_test.go` | 更新测试 |
| `internal/service/searcher.go` | 使用泛化 RRF |
| `internal/daemon/routes.go` | `handleMemorySearch` → `handleMemoryQuery` |
| `internal/daemon/client.go` | `MemorySearch` → `MemoryQuery` |
| `internal/daemon/server.go` | 路由 `/memory/search` → `/memory/query` |
| `internal/cli/memory.go` | `memory search` → `memory query`，去掉 `--type` flag |
| `internal/mcp/server.go` | `memory_search` → `memory_query` |

## 不涉及

- Memory 的写逻辑（Add 不变，仍按 type 写入）
- 文档搜索逻辑
- Embedding 模型/配置

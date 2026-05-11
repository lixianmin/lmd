# Per-Doc 线性管线设计

## 一、背景

现有管线拆成三个独立步骤（syncIndex → embedChunks → summarize），每个步骤各自读写 DB，互相依赖 DB 中间状态判断"该不该我干"。问题：

1. **流程不可读**：要理解完整流程，得在脑子里串起三段代码和 DB 状态
2. **测试成本高**：500 个文档进去，要等 embed 跑完才能测 summary 效果
3. **崩溃恢复复杂**：存在大量半成品状态（有 chunks 没 vectors、有 summary 没 chunks 等）
4. **大量无头组件**：Embedder 自己查 DB 找活干，Summarizer 自己查 DB 取 chunks

## 二、设计目标

1. **一个文档一条线走完**：读文件 → chunk → embed → summarize → 写 DB，中间不碰 DB
2. **可线性测试**：处理完一个文档，summary + chunks + vectors 全部就绪，立刻能测
3. **无半成品状态**：要么全有，要么全无，崩溃后重试即可
4. **内存可控**：大文件（1GB+）分批处理，不撑爆内存

## 三、架构

```
goLoop (单 ticker, 60s)
  │
  ├─ Scan 阶段：扫描文件系统，对比 DB，检测变更
  │   返回：[]PendingDoc（新增/修改/删除）
  │   不写 DB（仅读 DB 做对比，fileModTime 未变可跳过）
  │
  └─ Process 阶段：逐个处理 PendingDoc
      │
      ├─ 删除类：事务删除该文档所有数据（级联：chunks_vec + chunks_fts + chunks + documents + summary 相关）
      │
      └─ 新增/修改类：
          1. 如果修改：删除旧数据
          2. 读文件 → chunking（分批产出）
          3. 对每批 chunks：embed → 写 chunks + FTS + vectors
          4. summarize（LLM 调用，用截断内容）
          5. embed summary → 写 summary doc + summary chunk + summary FTS + summary vector
          6. 写 document 记录（含 hash + fileModTime）— 完成标记
```

## 四、Scan 阶段

### 职责

检测文件系统变化，返回待处理列表。不写 DB（仅读 DB 做对比）。

### 流程

对每个 collection：

1. 读 DB：获取该 collection 所有已索引文档，构建 `map[relPath]{docId, hash, fileModTime}`
2. Walk 文件系统，对每个匹配文件：
   - **未变化**（fileModTime 一致）→ 跳过
   - **新文件** → 读内容，compute hash，chunk → `PendingDoc{Action: DocNew, Chunks: [...]}`
   - **fileModTime 变了 + hash 也变了** → 读内容，chunk → `PendingDoc{Action: DocChanged, OldDocId: ..., Chunks: [...]}`
   - **fileModTime 变了 + hash 没变** → 跳过（内容未变，不需要重处理）
3. 对 DB 中存在但文件系统不存在的路径 → `PendingDoc{Action: DocDeleted, OldDocId: ...}`

### fileModTime 优化

fileModTime 未变的文件直接跳过，不读文件不计算 hash。这是性能关键路径——大部分文件每轮都没变。

fileModTime 变了但 hash 没变的文件也跳过。这种情况说明文件被 touch 了但内容没改。

### 返回值

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
    Body        string       // 全文内容（用于 summary 生成）
    Hash        string       // SHA256 of file bytes
    FileSize    int64
    FileModTime int64
    OldDocId    int64        // Changed/Deleted 时有值
    Chunks      []ChunkData  // 已切好的 chunks
}
```

## 五、Process 阶段

### 删除类

```
dao.DeleteDocument(OldDocId)
```

现有级联删除，事务内删除 chunks_vec + chunks_fts + chunks + documents。如果该文档有 summary（`@summaries` 中的 `source_doc_id = OldDocId`），也一并删除。

### 新增/修改类

```
func processDoc(doc PendingDoc) error:
    // 1. 修改时先删旧数据（包括 summary）
    if doc.Action == DocChanged:
        dao.DeleteDocumentAndSummary(doc.OldDocId)

    // 2. 写入 document 记录（不含 hash/fileModTime，作为"处理中"标记）
    //    拿到 docId，后续 chunks/summary 都需要这个 FK
    docId := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize)

    // 3. 分批 embed chunks + 写入
    for batch := range chunkBatches(doc.Chunks, batchSize):
        texts := [c.Content for c in batch]
        vecs := embedProvider.EmbedBatch(ctx, texts)
        dao.InsertChunksAndVectors(docId, batch, vecs, doc.Collection)
        // ↑ 每批提交一次，释放 vectors 内存

    // 4. summarize（用截断内容，不会爆内存）
    summary := llm.ChatCompletion(ctx, prompt(doc.Title, truncate(doc.Body)))

    // 5. embed summary + 写入
    summaryVecs := embedProvider.EmbedBatch(ctx, []string{summary})
    dao.InsertSummaryWithVector(docId, summary, summaryVecs[0])

    // 6. 更新 document 的 hash + fileModTime（完成标记）
    dao.CompleteDocument(docId, doc.Hash, doc.FileModTime)
```

### 两阶段 Document 写入

document 记录分两步写入，解决"先要 docId 做 FK"和"document 是完成标记"的矛盾：

1. **InsertDocument**（步骤 2）：写入 collection/path/title/body/fileSize，**不含 hash 和 fileModTime**。拿到 docId。
2. **CompleteDocument**（步骤 6）：更新 hash 和 fileModTime。

Scan 阶段判断逻辑：

- document 不存在 → New → 走流程
- document 存在，hash 为空 → 上次处理中断，先 DeleteDocumentAndSummary 清理，再重做
- document 存在，hash 不匹配文件 → Changed → 先 DeleteDocumentAndSummary，再重做
- document 存在，hash 匹配且 fileModTime 匹配 → Unchanged → 跳过

### 删除 Summary 的级联

修改文档时，除了删除原文档的 chunks/vectors，还要删除关联的 summary：

```sql
DELETE FROM chunks_vec WHERE chunk_id IN (SELECT id FROM chunks WHERE doc_id IN (
    SELECT id FROM documents WHERE source_doc_id = OldDocId
));
DELETE FROM chunks_fts WHERE rowid IN (...);
DELETE FROM chunks WHERE doc_id IN (...);
DELETE FROM documents WHERE source_doc_id = OldDocId;
```

## 六、DAO 层新增/修改

### 新增方法

| 方法 | 作用 |
|------|------|
| `InsertDocument(collection, path, title, body, fileSize) int64` | 写入 document 记录（不含 hash/fileModTime），返回 docId |
| `CompleteDocument(docId, hash, fileModTime)` | 更新 document 的 hash + fileModTime（完成标记） |
| `InsertChunksAndVectors(docId, chunks, vectors, collection)` | 事务：写入 chunks + FTS + vectors。每批调用一次 |
| `InsertSummaryWithVector(sourceDocId, summary, vector)` | 事务：写入 summary doc + summary chunk + FTS + vector |
| `DeleteDocumentAndSummary(docId)` | 事务：删除 document + chunks + vectors + summary 全部关联数据 |

### 可删除的方法

| 方法 | 原因 |
|------|------|
| `dao.GetUnembeddedChunks()` | 不再有独立 embed 流程 |
| `dao.GetUnembeddedCount()` | 同上 |
| `dao.UpsertSummaryDoc()` | 被 `InsertSummaryWithVector` 替代 |
| `dao.UpsertSummaryWithVector()` | 被新的 `InsertSummaryWithVector` 替代 |
| `dao.UpsertDocument()` | 被 `InsertDocument` 替代（不再 upsert，先删后插） |
| `dao.TouchDocument()` | 不再需要 |

### 保留不动的 DAO 方法

Searcher 相关的所有方法不变：
- `SearchFTS`, `SearchFTSBM25`, `SearchFTSByDocIds`
- `QueryVectors`, `QueryVectorsByCollection`, `QueryVectorsByDocIds`
- `GetChunksByDocId`, `GetChunksByIds`, `GetChunkById`
- `GetDocumentByDocId`, `GetDocumentById`, `GetDocumentByPath`
- Collection CRUD

## 七、被删除/重写的组件

| 组件 | 操作 |
|------|------|
| `service.Embedder` | **整个文件删除** |
| `service.Indexer` | **重写**：`UpdateCollection` 改为返回 `[]PendingDoc`，不写 DB |
| `service.Summarizer` | **重写**：改为接收 chunks 参数，不查 DB |
| daemon `embedTicker` + `embedChunks()` | **删除** |
| daemon `goLoop` | **简化**：单 ticker，scan → process 循环 |

## 八、goLoop 结构

```go
func (my *Daemon) goLoop(later loom.Later) {
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
                my.processDoc(doc)
            }
        }
    }
}
```

- 单 ticker，单 goroutine
- 无 embedTicker，无 pending slice
- scanChanges 返回什么就处理什么
- 每个 doc 处理前检查 closeChan（支持快速退出）

## 九、崩溃恢复

| 崩溃时机 | DB 状态 | 恢复 |
|----------|---------|------|
| API 调用中途 | document 存在但 hash 为空 | Scan 检测到 hash 为空 → 删后重做 |
| chunks 写入中途 | 部分 chunks + vectors，document hash 为空 | 同上 |
| summary 写入中途 | chunks 完整，无 summary，document hash 为空 | 同上 |
| CompleteDocument 后 | 全部完整，hash 已写入 | Scan 认为已处理，跳过 |

核心保证：**hash 字段是完成标记**。hash 为空或不匹配，就是未完成，先 DeleteDocumentAndSummary 清理再重做。

## 十、不动的部分

- **Searcher** — 搜索逻辑不变，仍然查 chunks + chunks_fts + chunks_vec + documents
- **DB schema** — 表结构不变（documents, chunks, chunks_fts, chunks_vec, collections）
- **Tokenizer** — 不变
- **Collection 管理**（add/remove/rename）— 不变
- **HTTP API** — 不变
- **MCP handler** — 不变

# Pipeline 统一：syncIndex → summarize → embed 串行化

## 一、问题

当前 `goLoop` 用三个独立 ticker 驱动三个阶段：

| ticker | 间隔 | 做什么 |
|--------|------|--------|
| syncIndexTicker | 60s | 扫描文件变更，产生 DirtyDocIds |
| summaryTicker | 300s | 从内存 dirty map 取 doc，调 LLM 生成摘要 |
| embedTicker | 5s | LEFT JOIN 扫描未嵌入 chunks，调 Embedding API |

问题：
1. **无因果保证**：syncIndex 产出 dirty docs 后，要等最长 300s summaryTicker 才触发处理；embedTicker 5s 扫一次大多数时候空转
2. **回调模式别扭**：summarize 完成后通过 `onUpsert` 回调起一个 `loom.Go` 做嵌入，进程崩了就丢
3. **状态丢失**：dirty map 纯内存，`popDirty()` 一次性弹出，处理中崩溃则未处理的 doc 丢失
4. **LLM 失败无重试**：失败后 doc 从 dirty map 弹出，不再重试直到下次 `ScanAll()`

## 二、设计目标

1. **串行 pipeline**：syncIndex → ProcessDirty(embed+summarize 合并) → embedChunks(原始 chunks)，顺序执行
2. **事务写入**：summary + embedding 在一个事务中写入，进程崩溃无半成品
3. **崩溃恢复**：重启后 `ScanAll()` 重新发现缺失 summary 的文档
4. **并发控制**：单循环天然串行，无并发问题

## 三、核心改动

### 3.1 goLoop 改为串行 pipeline

**Before：** 三个独立 ticker 并发触发

```go
for {
    select {
    case <-syncIndexTicker.C: my.syncIndex()
    case <-summaryTicker.C:   my.summarizer.ProcessDirty()
    case <-embedTicker.C:      my.embedChunks()
    }
}
```

**After：** 主 pipeline ticker 驱动 syncIndex → ProcessDirty，embedChunks 独立并行运行

```go
func (my *Daemon) goLoop(later loom.Later) {
    var tick = later.NewTicker(indexSyncInterval)
    var embedTicker = later.NewTicker(embedTickInterval)
    var closeChan = my.wc.C()

    for {
        select {
        case <-closeChan:
            return
        case <-tick.C:
            my.pipelineTick()
        case <-embedTicker.C:
            my.embedChunks()
        }
    }
}

func (my *Daemon) pipelineTick() {
    // Stage 1: 扫描文件变更，产生 DirtyDocIds
    my.syncIndex()

    // Stage 2: 处理脏文档（summarize + embed 合并，事务写入）
    my.summarizer.ProcessDirty()
}
```

`syncIndex → ProcessDirty` 必须串行：ProcessDirty 依赖 syncIndex 产出的 dirty docs。间隔使用现有的 `indexSyncInterval`（60s）。

`embedChunks` 独立运行：它通过 LEFT JOIN 扫描未嵌入的原始 chunks，不依赖 syncIndex 或 ProcessDirty。保留 `embedTicker`（5s），在 `select` 中与 pipeline ticker 并行触发。

### 3.2 summarizer.processDoc 合并 embed

**Before：** 调 LLM → 事务写 summary+FTS → fire onUpsert 回调 → 异步 embed

**After：** 调 LLM → 调 Embed API → 事务写 summary+FTS+vector

```go
func (my *Summarizer) processDoc(ctx context.Context, docId int64) error {
    // ... 读取源文档、拼接 chunks、截断（不变）

    summary, err := my.generateSummary(ctx, doc.Title, content)
    if err != nil {
        return err
    }

    // 新增：同步 embed 摘要文本
    vecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
    if err != nil {
        return err
    }

    // 事务写入 summary doc + chunk + FTS + vector
    my.upsertSummaryWithVector(docId, doc.Hash, summary, tokenizedSummary, vecs[0])
    return nil
}
```

Summarizer 需要持有 `embedProvider` 引用，通过构造函数注入。

### 3.3 新增 DAO: UpsertSummaryWithVector

在 `UpsertSummaryDoc` 基础上扩展，在同一事务内额外插入 vector：

```go
func UpsertSummaryWithVector(sourceDocId int64, hash, summary, tokenizedSummary string, vec []float32) (int64, error) {
    // 事务内：
    // 1. 删除旧的 summary doc + chunks + FTS + vectors（已有）
    // 2. 插入 summary document（已有）
    // 3. 插入 summary chunk（已有）
    // 4. 插入 FTS（已有）
    // 5. 插入 vector（新增）
}
```

### 3.4 删除的东西

| 删除 | 原因 |
|------|------|
| `summarizer.SetOnUpsert()` | 不再需要回调 |
| `summarizer.SetStopCh()` | ProcessDirty 由 pipeline 驱动，ctx 从外部传入 |
| `daemon.go` 中的 `SetOnUpsert(loom.Go(...))` 回调 | embedding 已内联 |
| `summaryTicker` | 不再需要独立 ticker，ProcessDirty 由 pipelineTick 驱动 |

### 3.5 embedChunks 独立并行

`embedChunks` 负责嵌入非 summary 的原始文档 chunks。它与主 pipeline（syncIndex → ProcessDirty）无依赖关系，通过独立的 `embedTicker`（5s）在 `select` 中并行触发。这确保即使 ProcessDirty 因 LLM 调用耗时长，原始 chunks 的嵌入也不被阻塞。

### 3.6 ProcessDirty 签名调整

增加 context 参数用于超时和取消：

```go
func (my *Summarizer) ProcessDirty(ctx context.Context)
```

### 3.7 cooldown 机制

现有 `summaryCooldown` 机制（等文件修改稳定后再 summarize）不再需要。原因是：
- 原来 cooldown 是因为 summaryTicker 固定间隔，需要在 ticker 触发时检查文件是否还在频繁修改
- 改为 pipeline 后，syncIndex 已通过 mtime+hash 检测文件是否真正变更，只有 hash 变了才标记 dirty
- 因此 ProcessDirty 里的每个 doc 都确实需要重新 summarize

删除 `SummaryConfig.CooldownSeconds` 配置。

## 四、崩溃恢复

| 场景 | 恢复方式 |
|------|---------|
| syncIndex 完成后，ProcessDirty 前崩溃 | dirty map 丢失，但重启后 `ScanAll()` 重新发现缺失 summary |
| LLM 调用中崩溃 | DB 无写入，重启后 `ScanAll()` 重试 |
| Embed API 调用中崩溃 | DB 无写入，重启后 `ScanAll()` 重试 |
| 事务写入中崩溃 | SQLite WAL 保证事务原子性，要么全写要么全不写 |
| 原始 chunks 未嵌入 | `embedChunks` 通过 LEFT JOIN 发现未嵌入 chunks，下轮 pipeline 处理 |

## 五、改动文件清单

| 文件 | 改动 |
|------|------|
| `internal/daemon/daemon.go` | `goLoop` 改为 pipeline ticker + embedTicker 并行；`pipelineTick()` 新方法；删除 `SetOnUpsert` 回调；删除 `summaryTicker`/`cooldownSeconds` |
| `internal/service/summarizer.go` | `processDoc` 加 embed 调用；新增 `embedProvider` 字段；构造函数接收 embedProvider；删除 `SetOnUpsert`/`SetStopCh`/`onUpsert`/`stopCh`；`ProcessDirty` 增加 ctx 参数 |
| `internal/dao/document.go` | 新增 `UpsertSummaryWithVector` 方法 |

## 六、不动的部分

- `ScanAll()` 机制 — 启动时恢复，逻辑不变
- `GetUnembeddedChunks()` — embedChunks(Stage 3) 继续使用
- `Indexer` — 不变
- `Embedder` — 不变（Stage 3 继续使用）
- DB schema — 不变
- 配置结构 — 仅删除 `cooldown_seconds`

## 七、实现状态

- [x] Task 1: UpsertSummaryWithVector DAO 方法 (4140ec7)
- [x] Task 2: Summarizer 重构 — embed 合并 (447713a)
- [x] Task 3: daemon pipelineTick + 删除 summaryTicker/cooldown (5f76ad5)
- [x] Task 4: 清理残留代码 (07b8e66)

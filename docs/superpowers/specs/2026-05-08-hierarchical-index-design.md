# 层级索引系统设计

## 背景

LMD 当前在多个 Collection 内做全量混合检索。随着 Collection 增多（工作、生活、心理、计算机……），全量搜索速度慢、召回精度低。需要一套层级索引系统：顶部摘要做路由，定位到具体目录后再做精细检索。

## 设计目标

1. 每个目录自动生成 `_topic.md` 摘要文件，不论深度
2. 查询时通过 embedding 向量路由，一次定位目标目录，再在该目录内做混合检索
3. 离线 LLM 批处理生成摘要和语义分组，在线查询不调用生成模型
4. 不影响现有 `/query` `/search` `/vsearch` 接口

---

## 设计

### 1. 物理结构

每个目录生成一个 `_topic.md`。规则统一，包括叶子目录（1-2个文件）。

```
~/notes/                         ← Collection "notes"
  ├── _topic.md                  ← 根摘要
  ├── db/
  │   ├── _topic.md
  │   ├── mysql-index.md
  │   └── query-plan.md
  └── network/
      ├── _topic.md
      └── tcp.md
```

```
~/.lmd/memories/                 ← Collection "记忆"（AI组织）
  ├── _topic.md
  ├── 技术问题/                   ← AI自动创建
  │   ├── _topic.md
  │   └── go-goroutine-leak.md
  └── 生活记录/
      ├── _topic.md
      └── doctor-appointment.md
```

### 2. `_topic.md` 格式

```markdown
# 数据库优化

> 本目录包含数据库性能优化资料，涵盖索引策略、查询优化、分库分表。

## 关键主题
- MySQL 索引优化
- 查询计划分析
- 分库分表

## 子目录
- `子目录名/` — 一句话描述

## 文档
- `mysql-index.md` — MySQL索引类型与使用场景
- `query-plan.md` — EXPLAIN 输出解读

## 语义分组
- **索引策略** (3篇): mysql-index.md, btree-hash.md, covering-index.md
- **查询优化** (2篇): query-plan.md, slow-query.md
```

- `>` 块引用 = 概述，用于 embedding 路由
- `## 关键主题` = 5-8 个核心主题词
- `## 子目录` = 子目录列表（如果有）
- `## 文档` = 本层所有文档列表
- `## 语义分组` = LLM 按内容自动划分的主题群

### 3. Single Point of Truth

`_topic.md` 文件是唯一真相源。SQLite `topics` 表是派生缓存：

```
_topic.md           ← 权威源（LLM写，人可编辑）
    │
    ▼ 解析 + 向量化
topics 表           ← 派生缓存（machine readable）
```

- `_topic.md` 丢失 → 无法从 DB 恢复（自然语言不可重建）
- DB 丢失 → 从 `_topic.md` 文件重新解析 + 向量化
- 用户手改 `_topic.md` → 下次 sync 检测到 hash 变化 → 跳过 LLM 覆盖，重新解析入库

### 4. Schema 变更

**新增 `topics` 表：**

```sql
CREATE TABLE IF NOT EXISTS topics (
    collection  TEXT NOT NULL,
    rel_path    TEXT NOT NULL,       -- 相对路径，"" 为根目录
    overview    TEXT NOT NULL,       -- _topic.md 的概述部分（用于 embedding 路由）
    doc_paths   TEXT NOT NULL,       -- JSON [], 目录下所有文档路径
    hash        TEXT NOT NULL,       -- _topic.md 文件内容 SHA-256，检测人为修改
    updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
    PRIMARY KEY (collection, rel_path)
);
```

**新增 `topics_vec` 向量表：**

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS topics_vec USING vec0(
    topic_rowid INTEGER PRIMARY KEY,
    overview_vector float[1024] distance_metric=cosine
);
```

### 5. 配置变更

`config.go` 新增 Topic 配置段：

```go
type TopicConfig struct {
    SummarizeModel   string `yaml:"summarize_model"`    // 摘要模型 GGUF 路径
    SummarizeGPULayers int   `yaml:"summarize_gpu_layers"`
    SummarizeThreads int    `yaml:"summarize_threads"`
    CooldownSeconds  int    `yaml:"cooldown_seconds"`   // 摘要冷却期，默认300
    MaxDocCount      int    `yaml:"max_doc_count"`      // 每个 _topic.md 最多包含文档数，默认50，超过则拆子目录
}
```

### 6. 摘要生成（离线）

**触发时机**：daemon syncIndex（60s 间隔）检测到目录文件变更后，等待冷却期（默认 5 分钟），然后调用 LLM 生成。

**生成流程**：

```
syncIndex 检测文件变更
    ↓ 更新 documents/chunks（现有流程）
    ↓ 标记脏目录（文件新增/删除/修改）
    ↓ 等待冷却期（最后写入时间 + 300s）
    ↓ 遍历脏目录：
    ↓   收集目录下所有文档的 title + 前 200 字
    ↓   组装 prompt → LLM 生成 _topic.md
    ↓   计算 _topic.md SHA-256 存入 DB hash 字段
    ↓   解析 _topic.md → 提取 overview → 向量化 → 写入 topics_vec
    ↓   提取文档列表 → 写入 topics.doc_paths
```

**Prompt 模板**：

```
你是一个知识库索引助手。请阅读以下目录中的文档标题和摘要，生成一个 _topic.md 索引文件。

目录路径: {dir_path}
文档数量: {N}

文档列表:
--- doc1.md: {title}
{first_200_chars}
--- doc2.md: {title}
{first_200_chars}
...

请按以下格式生成 _topic.md：

# <简短目录标题>
> <2-3句概述>
## 关键主题
- <5-8个核心主题词>
## 文档
- `filename.md` — <一句话描述>
## 语义分组
- **<分组名>** (N篇): file1.md, file2.md, ...
```

**覆盖保护**：生成前比较 topics.hash。如果文件已被用户手动修改（hash 不匹配），跳过覆盖，仅日志告警。

### 7. 查询路由（在线）

新增端点 `POST /smart-query`：

```
query → embed(query)                       // 1次 embedding 调用（现有模型）
     → 向量搜索 topics_vec                 // Top-3 目录，亚毫秒
     → 合并 3 个目录的 doc_paths           // 得到候选文档白名单
     → 在白名单内做混合检索(BM25+向量+RRF)  // 复用现有 searcher
     → 返回 SearchHit 列表                  // 格式与 /query 一致
```

- 某个目录的 `_topic.md` 不存在 → 该目录不参与路由（后台异步生成）
- 所有目录的 `_topic.md` 都不存在 → 降级为普通 `/query`（全库搜索）

### 8. 数据流总图

```
                              ┌─────────────┐
                              │  文件系统    │
                              │ (markdown)  │
                              └──────┬──────┘
                                     │
                    ┌────────────────┼────────────────┐
                    ▼                ▼                ▼
              ┌──────────┐   ┌──────────┐    ┌──────────┐
              │ syncIndex │   │ LLM生成  │    │ 人手编辑 │
              │ (60s)     │   │ _topic.md│    │ _topic.md│
              └──────────┘   └────┬─────┘    └────┬─────┘
                    │              │               │
                    ▼              ▼               │
              ┌──────────────────────────┐         │
              │       topics 表          │◄────────┘
              │  (parse + embed + hash)  │
              └──────────┬───────────────┘
                         │
                         ▼
              ┌──────────────────────┐
              │    topics_vec        │
              │ (overview embedding) │
              └──────────────────────┘
                         │
                         ▼
              ┌──────────────────────┐
              │   /smart-query       │
              │  embed → route →     │
              │  search → return     │
              └──────────────────────┘
```

### 9. 新增文件

| 文件 | 职责 |
|------|------|
| `internal/service/topic_indexer.go` | 摘要生成（LLM 调用 + _topic.md 写入 + topics 表解析入库） |
| `internal/dao/topic.go` | topics 表 CRUD + 向量写入 |
| `internal/service/topic_router.go` | 查询路由（embed → vector match → doc filter） |
| `internal/service/llm_client.go` | LLM 生成客户端（llama-go 加载 Qwen3-4B-Instruct） |

### 10. 未纳入本设计的内容

- AI 自动组织目录结构（移动文件、创建子目录）—— 后续 spec
- 记忆时间衰减 —— 已有独立 spec
- 摘要任务的优先级队列 —— 先串行满足基本需求
- `_topic.md` 的 YAML front matter 格式 —— 当前用纯 Markdown 足够

---

## 风险与对策

| 风险 | 对策 |
|------|------|
| LLM 摘要质量差 | 摘要只用于路由，最终检索仍是混合搜索。路由错也有 Top-3 兜底 |
| 首次启动无摘要 | 降级为全库搜索，同时后台生成 |
| 大量目录时生成慢 | 单目录串行 + 冷却期限制频率 |
| embedding 模型和生成模型同时占用内存 | 互斥加载：embedding 模型 idle 释放后加载生成模型；或通过 CPU offloading 共存 |

# HyDE 索引设计

## 背景

原有的两级搜索方案：Level 1 搜 `@summaries`（LLM 生成的文档概括）→ Level 2 精搜匹配到的源文件。

**问题**：大海捞针场景下，summary 会丢失关键细节（如"卧室刷成浅灰色"埋在一段关于室内植物的对话里）。即使换更大的模型或改 prompt，summary 本质是压缩，压缩必然丢信息。

**新思路**：不概括，改为生成"假设查询"。每个文档生成 5-10 个可能被问到的问题，加上从原文提取的高惊奇度关键词，存入 `@hyde` 集合。Level 1 搜这些假设查询和关键词。

## 设计目标

- 用 7B 小模型生成假设查询（成本低）
- 用算法提取高惊奇度关键词（不依赖 LLM）
- 复用现有 `documents/chunks/chunks_fts/chunks_vec` 存储基础设施
- Level 1 搜 `@hyde` → Level 2 精搜源文件 → 回退全局搜索
- 替换现有的 `lmd hyde` 命令为两级搜索

## 架构

```
Daemon
├── ChunkIndexer (重命名自 Indexer) — chunking + embedding
├── HyDEIndexer (新建) — 生成 HyDE 数据
└── 路由层 — /hyde 走两级搜索
```

## 数据存储：`@hyde` 集合

复用现有表结构，collection = `@hyde`。

每个源文档 → 一条 `@hyde` 记录：
- `documents.source_doc_id` → 源文件 ID
- `chunks[0].content` → 拼接后的文本（假设查询 + 关键词）
- `chunks_fts` → FTS 索引
- `chunks_vec` → embedding 向量（一次 embed 整段文本）

拼接格式：
```
QUESTIONS:
What color did I repaint my bedroom walls?
What plants are recommended for low-light conditions?
How can I design a home office nook for productivity?
KEYWORDS:
bedroom, gray, lighter shade, snake plant, ZZ plant, ...
```

## HyDEIndexer

```go
type HyDEIndexer struct {
    llm       llm.LLMProvider
    embedProv embedding.EmbeddingProvider
    cfg       config.HydeConfig
}
```

### 处理流程

对每个源文档：
1. 调 7B 模型，prompt：生成 5-10 个关于此文档可能被问到的问题
2. 用算法从原文档提取高惊奇度关键词（见下）
3. 拼接假设查询 + 关键词
4. 调 embedding API 向量化
5. 存入 `@hyde` 集合

### 7B 模型 Prompt

```
Given the document below, generate 5-10 questions that this document could answer.
Focus on specific facts, details, and information mentioned in the text.
One question per line. Be specific — ask about names, numbers, places, dates, preferences.

Document:
{content}

Questions:
```

### 高惊奇度关键词提取（算法，不用 LLM）

使用现有 tokenizer 分词：
1. 对文档分词
2. 排除停用词（常见英文/中文虚词）
3. 按词频降序排列
4. 取 top-30 高频且长度 > 2 的词
5. 额外提取所有数字（含百分号、单位）和专有名词（大写开头的英文词）

## 搜索流程：`lmd hyde <query>`

```
用户输入 query
    ↓
Level 1: 在 @hyde 集合中 hybrid search (FTS + vector)
    ↓ 找到匹配
提取 source_doc_id 列表
    ↓
Level 2: 在源文件 chunks 中 hybrid search（限定 docIds）
    ↓
返回精排结果

Level 1 无匹配 → 回退全局 hybrid search
```

回退等价于 `lmd hybrid`，确保不低于基线。

## Hybrid 命名统一（Query → Hybrid）

当前"混合检索"（FTS + vector）的命名混乱，有的叫 `query`，有的叫 `hybrid`，将来新增检索方案时容易产生歧义。本次统一为 `hybrid`，使其成为独立稳定的模块。

### 重命名清单

| 层 | 原 | 新 |
|---|---|---|
| CLI 命令 | `lmd query` | `lmd hybrid` |
| CLI 变量 | `queryCmd` | `hybridCmd` |
| 路由 | `POST /query` | `POST /hybrid` |
| 路由 handler | `handleQuery` | `handleHybrid` |
| Client 方法 | `Query()` | `Hybrid()` |
| Bench backend | `"query"` | `"hybrid"` |
| MCP tool | `"query"` | `"hybrid"` |

JSON 请求体中的 `"query"` 字段保持不变（那是查询文本，不是搜索方式名）。

## 代码改动清单

### 删除
- `smart-query` CLI 命令和路由
- `handleSmartQuery` 路由方法
- Processor 中的 `generateSummary` 逻辑
- `config.SummaryConfig` — 替换为 `config.HydeConfig`

### 新建
- `internal/service/hyde_indexer.go` — HyDEIndexer 结构体
  - `ScanChanges()` — 检测需要生成 HyDE 数据的文件
  - `Process(change)` — 生成假设查询 + 提取关键词 + 存储
  - `generateQuestions(ctx, content)` — 调 7B 生成假设查询
  - `extractKeywords(content)` — 算法提取关键词

### 重命名
- `internal/service/indexer.go` — `Indexer` → `ChunkIndexer`
- `internal/cli/search.go` — `queryCmd` → `hybridCmd`，命令 `query` → `hybrid`
- `internal/daemon/server.go` — 路由 `POST /query` → `POST /hybrid`
- `internal/daemon/daemon_routes.go` — `handleQuery` → `handleHybrid`
- `internal/daemon/client.go` — `Query()` → `Hybrid()`，URL `/query` → `/hybrid`
- `internal/daemon/daemon_mcp.go` — MCP tool `"query"` → `"hybrid"`
- `internal/cli/bench.go` — backend `"query"` → `"hybrid"`，route `/query` → `/hybrid`

### 修改
- `internal/config/config.go` — `SummaryConfig` → `HydeConfig`，`Summary` → `Hyde`
- `internal/daemon/daemon.go` — `newLLM` 改用 `config.Hyde`；嵌入 ChunkIndexer 和 HyDEIndexer
- `internal/daemon/daemon_routes.go` — `handleHyde` 改为两级搜索逻辑（替换原单次 HyDE 生成）
- `internal/dao/document.go` — `UpsertSummaryWithVector` 重命名为 `UpsertHydeData`
- `internal/service/processor.go` — 去掉 summary 生成逻辑
- `internal/cli/search.go` — `hyde` 命令改为两级搜索

### 清理
- 清空 `@summaries` 集合数据

## 配置

```yaml
hyde:
    provider: siliconflow
    model: Qwen/Qwen2.5-7B-Instruct
    max_output_tokens: 512
    max_input_tokens: 30000
```

原 `summary` 配置段废弃，替换为 `hyde` 配置段。

## 验证标准

1. 对 longmemeval 前 29 题，`lmd hyde` 的 R@10 不低于 `lmd hybrid`（79.3%）
2. `lmd hybrid` 命令行为不变（仅改名）

## 成本估算

- 23867 个文件 × 1 次 LLM 调用（~2000 input tokens + ~200 output tokens）
- siliconflow 7B: ¥0.4/M tokens → 总计约 ¥20
- embedding: 23867 × 1 次（假设查询+关键词拼接文本 ~500 tokens）
- 时间：约 2-3 小时（受 API rate limit 限制）

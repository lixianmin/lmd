# LLM Provider 架构重构 & 文件级 Summary 索引

## 一、背景

当前 lmd 内嵌 llama.cpp 做 embedding 和文本生成，存在以下问题：
1. 内存/GPU 资源需自行管理，模型加载释放复杂
2. CGo 编译依赖重，跨平台迁移困难
3. 无法灵活切换模型服务（Ollama、硅基流动等）

同时，现有的 `_topic.md` 目录级摘要索引粒度不够细，检索召回有待提升。

## 二、设计目标

1. 移除 llama.cpp 依赖，统一为可插拔的 provider 接口
2. Embedding 和 LLM 生成各自独立接口，支持 Ollama / SiliconFlow
3. 废弃 `_topic.md` 目录级摘要，改为文件级 summary
4. 复用现有 chunks 表存储 summary，BM25 + Vector + RRF 检索逻辑只有一份
5. 清除所有记忆相关代码，后续重新设计

---

## 三、Provider 架构

### 3.1 目录结构

```
internal/provider/
├── provider.go          # LLMProvider 接口
├── ollama_llm.go        # OllamaLLM
├── siliconflow_llm.go   # SiliconFlowLLM

internal/embedding/
├── provider.go          # EmbeddingProvider 接口 (已有)
├── ollama.go            # → OllamaEmbedding
├── siliconflow.go       # → SiliconFlowEmbedding (新增)
├── mock.go              # 保留
```

### 3.2 接口定义

```go
// internal/provider/provider.go
type Message struct {
    Role    string // "system", "user", "assistant"
    Content string
}

type LLMProvider interface {
    ChatCompletion(ctx context.Context, messages []Message) (string, error)
    Close() error
}
```

EmbeddingProvider 接口保持不变：

```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    EmbedQuery(ctx context.Context, text string) ([]float32, error)
    Dimension() int
    ModelName() string
    Close() error
}
```

### 3.3 实现

**OllamaEmbedding**: 调用 `POST /api/embed`，复用现有 `ollama.go`。

**SiliconFlowEmbedding**: 调用 `POST /v1/embeddings`，标准 OpenAI 兼容 API。

**OllamaLLM**: 调用 `POST /api/chat`，Ollama Chat Completion。

**SiliconFlowLLM**: 调用 `POST /v1/chat/completions`，OpenAI 兼容格式。

---

## 四、配置结构

Provider 和功能正交：

```yaml
# === 服务提供商（URL/API Key 配一次） ===
providers:
  ollama:
    url: http://localhost:11434
  siliconflow:
    url: https://api.siliconflow.cn/v1
    api_key: sk-xxx

# === 功能：Embedding ===
embedding:
  provider: ollama                   # 引用 providers 中的 key
  model: qwen3-embedding
  batch_size: 8

# === 功能：Summary ===
summary:
  provider: siliconflow              # 引用 providers 中的 key
  model: Qwen/Qwen2.5-7B-Instruct
  max_tokens: 512
  cooldown_seconds: 300

# === 功能：HyDE（可选，后续实现） ===
hyde:
  provider: siliconflow
  model: Qwen/Qwen2.5-7B-Instruct
  max_tokens: 200

database:
  path: ~/.cache/lmd/index.sqlite

daemon:
  port: 12345
  index_poll_interval: 30s
```

Config 结构体：

```go
type Config struct {
    Providers ProviderConfig `yaml:"providers"`
    Embedding EmbeddingConfig `yaml:"embedding"`
    Summary   SummaryConfig   `yaml:"summary"`
    Database  DatabaseConfig  `yaml:"database"`
    Daemon    DaemonConfig    `yaml:"daemon"`
}

type ProviderConfig struct {
    Ollama      ProviderItem `yaml:"ollama"`
    SiliconFlow ProviderItem `yaml:"siliconflow"`
}

type ProviderItem struct {
    URL    string `yaml:"url"`
    APIKey string `yaml:"api_key,omitempty"`
}

type EmbeddingConfig struct {
    Provider  string `yaml:"provider"`  // "ollama" or "siliconflow"
    Model     string `yaml:"model"`
    BatchSize int    `yaml:"batch_size"`
}

type SummaryConfig struct {
    Provider        string `yaml:"provider"`
    Model           string `yaml:"model"`
    MaxTokens       int    `yaml:"max_tokens"`
    CooldownSeconds int    `yaml:"cooldown_seconds"`
}
```

删除的配置段：`llama`、`topic`、`ollama`（旧的顶层 ollama 配置）。

---

## 五、文件级 Summary 数据模型

### 5.1 复用 chunks 表

不新增任何表。文件 summary 作为 `@summaries` 系统 collection 中的 document + chunk：

```
documents 表：
  {id: 1,  collection: "notes",     path: "db/mysql-index.md", body: "## MySQL索引\n\n...", ...}
  {id: 99, collection: "@summaries", path: "notes/db/mysql-index.md", body: "", ...}

chunks 表：
  {doc_id: 1,  content: "MySQL索引简介...",  seq: 1, ...}   ← 原始文件分块
  {doc_id: 1,  content: "B+树结构...",       seq: 2, ...}
  {doc_id: 99, content: "MySQL索引优化文档，涵盖B+树结构、索引类型与使用场景。", seq: 1, ...}
                      ↑ summary 文本，该 document 的唯一 chunk
```

- `@summaries` 是系统 collection，名称以 `@` 开头，用户不可创建/删除
- file sync 时跳过 `@` 开头的 collection
- summary document 的 `body` 为空，content 全在 chunk 中
- 现有 embedder 自动对 `@summaries` 的 chunk 建 FTS5 + 向量索引

### 5.2 Summary 与原始文件的关联

通过 `path` 字段关联：summary document 的 `path` = 原始文件 document 的 `collection + "/" + path`。

或者更显式：利用 `original` 表或一个 `source_doc_id` 字段。最简方案：通过 path 匹配。

Level 2 检索时，从 `file_summaries`（即 `@summaries` 的 chunks）命中后，用 path 反查原始文件 doc_id。

---

## 六、两级检索

```
POST /smart-query {query: "MySQL索引优化", collection: "notes"}

  Level 1: 定位文件
    embed(query) → query_vector
    Searcher.SearchHybrid(query, collection="@summaries")
      → RRF(BM25 + Vector) over @summaries chunks
      → Top-K summary chunks
      → 通过 path 反查原始文件 doc_id → [doc_id_1, doc_id_2, ...]

  Level 2: 文件内检索
    Searcher.SearchHybrid(query, docFilter=[doc_id_1, doc_id_2, ...])
      → RRF(BM25 + Vector) over chunks (filtered by doc_id IN (...))
      → SearchHit 列表
```

两次搜索调用**同一个 `Searcher.SearchHybrid()`**，BM25 + Vector + RRF 只有一份代码。区别仅是：
- Level 1: 限定 `collection="@summaries"`
- Level 2: 限定 `doc_id` 白名单

降级策略：
- `@summaries` 无 summary → 降级为普通全库混合检索（当前 `/query` 行为）
- Level 1 无结果 → 降级为普通全库混合检索

---

## 七、Summary 生成

### 7.1 触发时机

复用现有 topic sync 冷却期机制：

```
syncIndex 检测文件变更
  → 更新 documents/chunks（现有流程）
  → 标记脏 doc_id（新增/修改的文件）
  → 等待冷却期（最后写入时间 + cooldown_seconds）
  → 对脏 doc_id 逐个检查：
      → 计算文件内容 SHA-256
      → 与 @summaries 中已有的 hash 比对
      → 相同 → 跳过
      → 不同 → 调用 LLMProvider.ChatCompletion() 生成 summary
      → 写入 @summaries document + chunk
      → embedder 自动建 FTS5/Vector 索引
```

### 7.2 Prompt

```
你是一个知识库索引助手。阅读以下文档，用1-2句话(不超过100字)概括其内容和核心主题。

文档标题: {title}
文档内容:
{前500字截断}

请直接输出摘要，不要加前缀和引号。
```

### 7.3 新增文件

| 文件 | 职责 |
|------|------|
| `internal/service/summarizer.go` | Summary 生成调度（替代 topic_indexer.go） |

---

## 八、删除清单

### 8.1 文件

| 文件 | 原因 |
|------|------|
| `llama-go/` (submodule) | 不再内嵌 llama.cpp |
| `internal/embedding/llama.go` | 改用 Ollama/SiliconFlow embedding |
| `internal/service/llm_client.go` | 改用 provider.LLMProvider |
| `internal/service/topic_indexer.go` | 废弃 _topic.md，改用 summarizer.go |
| `internal/service/topic_router.go` | 废弃 topics 表路由 |
| `internal/dao/topic.go` | topics 表废弃 |

### 8.2 表

| 表 | 原因 |
|------|------|
| `topics` | 废弃 _topic.md |
| `topics_vec` | 废弃 _topic.md |

### 8.3 配置字段

- `LlamaConfig` 整个结构体
- `TopicConfig` 整个结构体
- 旧的顶层 `ollama` 配置段

### 8.4 依赖

- `go.mod` 中的 `github.com/tcpipuk/llama-go` + replace directive
- `.gitmodules` 中的 submodule 条目
- `Makefile` 中 llama-go 编译步骤

---

## 九、记忆系统清理

1. 删除 `internal/dao/memory.go`
2. 删除 `internal/service/memory.go`
3. 删除 `internal/cli/memory.go` 中 memory 子命令
4. 删除 `internal/daemon/daemon_routes.go` 中 `/memory/*` 路由
5. 删除 `internal/mcp/server.go` 中 memory tools
6. 删除 `memories` / `memories_fts` / `memories_vec` 表定义
7. 将历史记忆讨论记录整理到单独归档文件

---

## 十、归档操作

1. 从当前 HEAD 切出 `archive/llamacpp` 分支
2. 打 annotated tag: `git tag -a v0.1-llamacpp -m "最后一个内嵌 llama.cpp 的版本"`
3. 切回原分支，执行上述删除和重构

---

## 十一、风险与对策

| 风险 | 对策 |
|------|------|
| Ollama/SiliconFlow 服务不可用 | 启动时检测 provider 可用性，报错并提示用户 |
| Summary 生成质量差 | Summary 只做路由，最终检索仍是混合搜索，Top-K 兜底 |
| Summary LLM 调用费用（SiliconFlow 按 token 计费） | 冷却期 + hash 去重，只对变更文件重新生成 |
| 首次启动无 summary | 降级为全库混合检索，同时后台生成 |

---

## 十二、未纳入

- HyDE provider 化（已有 hyde_api.go 走外部 API，暂不改动）
- 记忆系统重新设计（后续独立 spec）
- Reranker（后续独立 spec）

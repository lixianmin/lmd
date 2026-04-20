# LMD: 内嵌 llama.cpp Embedding + HyDE 生成

## Overview

用 `https://github.com/tcpipuk/llama-go`（llama.cpp Go CGo bindings）替换 Ollama HTTP API，daemon 进程内直接加载 GGUF 模型做 embedding 和 HyDE 文本生成。Ollama 代码保留但不启用。

## Motivation

1. **零外部依赖**：不需要安装和运行 Ollama，daemon 进程自己搞定一切
2. **更快的启动**：省去 HTTP 开销，embedding 直接在进程内完成
3. **统一生命周期**：daemon 统一管理模型加载/释放，不需要跨进程通信

## Architecture

```
┌──────────────────────────────┐
│  lmd daemon                  │
│                              │
│  ┌─ HTTP API server          │
│  ├─ MCP server               │
│  ├─ Indexer (poll 30s)       │
│  ├─ Embedder (background)    │
│  │   └─ llama-go embedding   │ ← 进程内，不走 HTTP
│  ├─ Searcher                 │
│  │   └─ HyDE via llama-go    │ ← 进程内，不走 HTTP
│  ├─ Collection manager       │
│  ├─ Memory layer             │
│  └─ ModelLifecycle           │ ← 新增：按需加载 / 空闲释放
│      ├─ embedModel (~600MB)  │   Qwen3-Embedding-0.6B-Q8_0.gguf
│      └─ hydeModel  (~600MB)  │   Qwen3-0.6B-Q8_0.gguf
└──────────────────────────────┘
```

## Design Decisions

### 1. 使用 tcpipuk/llama-go

- go-skynet/go-llama.cpp 的活跃 fork，持续跟踪 llama.cpp 上游
- 原生 batch embedding API：`GetEmbeddingsBatch(texts []string) ([][]float32, error)`
- Model/Context 分离设计：一个 model 可创建多个 context
- 支持 Metal (Apple Silicon M4) GPU 加速
- `WithParallel(8)` 控制并行序列数，0.6B 小模型在 M4 上 batch embedding 很快

集成方式：Git submodule + go.mod replace directive

### 2. Ollama 代码保留

- `internal/embedding/ollama.go` 保留，不删除
- 当前版本不支持切换，代码注释掉不走
- 未来如需支持 Ollama，取消注释 + 加 provider 配置即可

### 3. 模型生命周期管理

按需加载 + 空闲释放：

1. daemon 启动时不加载模型
2. 首次 embedding 或 HyDE 请求时，加载对应模型
3. 每次使用更新 last-active 时间戳
4. 后台 goroutine 每 30s 检查，如果超过 10 分钟未使用，调用 `model.Close()` 释放 GPU 内存
5. 下次使用时重新加载

加载一个 0.6B Q8_0 模型约需 1-2 秒。

### 4. 模型自动下载

首次启动时检查 `~/.cache/lmd/models/` 目录：

1. 如果 GGUF 文件存在，直接使用
2. 如果不存在，自动下载：
   - 先试 huggingface.co（官方）
   - 失败再试 hf-mirror.com（中国镜像）
3. 下载完成后继续启动

模型文件：
- Embedding: `Qwen3-Embedding-0.6B-Q8_0.gguf`（约 640MB）
- HyDE: `Qwen3-0.6B-Q8_0.gguf`（约 640MB）

### 5. HyDE 文本生成

用 llama-go 的 `ctx.Generate()` 替换 Ollama chat API：

```
model, err := llama.LoadModel(hydeModelPath, llama.WithGPULayers(-1))
ctx, err := model.NewContext(llama.WithContext(2048))
prompt := "Given query '{q}', write a short passage that would answer this query"
result, err := ctx.Generate(prompt, llama.WithMaxTokens(200))
```

## Config Changes

```yaml
daemon:
  port: 12345
  idle_timeout: 30m
  index_poll_interval: 30s

llama:
  embed_model: ~/.cache/lmd/models/Qwen3-Embedding-0.6B-Q8_0.gguf
  hyde_model: ~/.cache/lmd/models/Qwen3-0.6B-Q8_0.gguf
  gpu_layers: -1           # -1 = 全部 offload 到 GPU (Metal)
  model_idle_timeout: 10m  # 模型空闲释放时间
  parallel: 8              # 并行序列数 (batch embedding)
  threads: 4               # CPU 线程数

embedding:
  batch_size: 8            # 每批 embed 的 chunk 数
  truncation: 800          # rune 级截断

hyde:
  enabled: true
  max_tokens: 200          # HyDE 生成最大 token 数

vector:
  dimensions: 1024
  distance_metric: cosine

database:
  path: ~/.cache/lmd/index.sqlite
```

### Config Struct

```go
type Config struct {
    Daemon    DaemonConfig    `yaml:"daemon"`
    Llama     LlamaConfig     `yaml:"llama"`
    Embedding EmbeddingConfig `yaml:"embedding"`
    HyDE      HyDEConfig      `yaml:"hyde"`
    Vector    VectorConfig    `yaml:"vector"`
    Database  DatabaseConfig  `yaml:"database"`
}

type LlamaConfig struct {
    EmbedModel       string `yaml:"embed_model"`
    HydeModel        string `yaml:"hyde_model"`
    GPULayers        int    `yaml:"gpu_layers"`
    ModelIdleTimeout string `yaml:"model_idle_timeout"`
    Parallel         int    `yaml:"parallel"`
    Threads          int    `yaml:"threads"`
}

type EmbeddingConfig struct {
    BatchSize  int `yaml:"batch_size"`
    Truncation int `yaml:"truncation"`
}

type HyDEConfig struct {
    Enabled   bool `yaml:"enabled"`
    MaxTokens int  `yaml:"max_tokens"`
}
```

注意：
- `LlamaConfig` 是顶层配置，和 embedding/hyde 并列，因为模型是共享资源
- `EmbeddingConfig` 只保留业务参数（batch_size, truncation），不包含模型路径
- `HyDEConfig` 只保留开关和生成参数，不包含模型路径
- 移除了 `OllamaConfig` 和 `provider` 字段（Ollama 代码保留但不启用）

## Files Changed

### New Files
- `internal/embedding/llama.go` — LlamaProvider（实现 EmbeddingProvider 接口）
- `internal/service/model_lifecycle.go` — 模型按需加载 + 空闲释放

### Modified Files
- `internal/config/config.go` — 重构：LlamaConfig 顶层，移除 OllamaConfig，首次运行自动生成配置文件
- `internal/daemon/daemon.go` — 创建 LlamaProvider，启动 model lifecycle goroutine
- `internal/service/hyde.go` — HyDE 生成用 llama-go ctx.Generate()
- `internal/daemon/routes.go` — handleQuery 中 HyDE 走内嵌
- `go.mod` — 加 replace directive 指向 submodule
- `Makefile` — 加 llama-go submodule 初始化 + libbinding.a 编译步骤
- `.gitmodules` — 加 tcpipuk/llama-go submodule

### Unchanged Files
- `internal/embedding/ollama.go` — 保留不动
- `internal/embedding/provider.go` — 接口不变

## Implementation Order

1. **添加 submodule + 集成** — `git submodule add`, `go.mod replace`, 验证编译
2. **Config 重构** — LlamaConfig 顶层，移除 OllamaConfig，更新默认值
3. **LlamaProvider** — 实现 EmbeddingProvider 接口，单测
4. **ModelLifecycle** — 按需加载 + 空闲释放 goroutine
5. **模型下载器** — 自动从 huggingface/hf-mirror 下载 GGUF
6. **HyDE 内嵌** — 用 llama-go Generate 替换 Ollama chat
7. **Daemon 集成** — 替换 provider，启动 model lifecycle
8. **Makefile** — 编译前自动初始化 submodule + 编译 libbinding.a

## Testing

1. Unit: LlamaProvider.Embed / EmbedBatch / EmbedQuery
2. Unit: ModelLifecycle 加载/释放/超时
3. Unit: 模型下载（mock HTTP server）
4. Integration: daemon 启动 → 自动下载 → embedding → 搜索
5. Integration: 空闲 10 分钟后模型释放，下次请求重新加载

## Risk & Mitigations

| 风险 | 缓解 |
|------|------|
| CGo 编译环境复杂 | Makefile 自动处理；报错给用户清晰提示 |
| 两个模型占用 1.2GB VRAM | 按需加载 + 空闲释放，不需要同时常驻 |
| llama-go submodule 更新 | 手动 `git submodule update --remote` |
| macOS Metal 兼容性 | llama-go 原生支持 Metal（BUILD_TYPE=metal）；M4 测试验证 |

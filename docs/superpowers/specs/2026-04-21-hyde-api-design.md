# HyDE: 三方 API 替代本地模型

## 背景

本地 Qwen3-0.6B 做 HyDE 效果差：生成 30s、输出质量低、`/no_think` 不被遵守。
改用三方 API（硅基流动等 OpenAI-compatible）的更大模型。

Embedding 仍使用本地 llama-go，本次不涉及。

## 配置

`~/.config/lmd/config.yaml` 新增：

```yaml
hyde:
  base_url: "https://api.siliconflow.cn/v1"
  api_key: ""
  model: "Qwen/Qwen3.5-9B"
  max_tokens: 200
```

- 无 `enabled` 字段 —— HyDE 只在 `lmd hyde` 命令触发，按需调用
- `api_key` 为空时，`lmd hyde` 返回错误提示用户配置

## 架构变更

### 移除

| 文件 | 原因 |
|------|------|
| `internal/service/hyde_llama.go` | 本地 LLM HyDE，不再使用 |
| `internal/service/hyde_llama_test.go` | 对应测试 |
| daemon 中 HyDE 模型下载 | 不需要本地 HyDE 模型文件 |
| daemon 中 `hydeLifecycle` | 三方 API 无需空闲释放 |
| config 中 `HyDE.Enabled` | 无需开关 |

### 新增

| 文件 | 内容 |
|------|------|
| `internal/service/hyde_api.go` | `HyDEAPIClient`，调 OpenAI-compatible `/chat/completions` |
| `internal/service/hyde_api_test.go` | 用 httptest mock 测试 |

### HyDEAPIClient 设计

```go
type HyDEAPIClient struct {
    baseURL   string
    apiKey    string
    model     string
    maxTokens int
}

func NewHyDEAPIClient(baseURL, apiKey, model string, maxTokens int) *HyDEAPIClient
func (my *HyDEAPIClient) Generate(ctx context.Context, query string) (string, error)
```

- `Generate` 调 `POST {base_url}/chat/completions`
- 超时 60s
- 请求格式遵循 OpenAI Chat Completions API
- Prompt: `Write a brief factual passage (50-150 words)...`（通过 `enable_thinking: false` JSON 字段禁用思考，非 `/no_think` 前缀）

### daemon 接线

```go
// daemon.go Start()
my.hydeGen = service.NewHyDEAPIClient(
    cfg.HyDE.BaseURL, cfg.HyDE.APIKey, cfg.HyDE.Model, cfg.HyDE.MaxTokens,
)
```

- 无 lifecycle goroutine
- 无模型下载
- 无 HyDE 相关 import（llama）

### handleHyde 路由行为

- `api_key` 为空 → `Generate()` 返回错误 `"HyDE requires api_key, set hyde.api_key in config"`
- 正常 → 调 API 生成假想文档 → embedding → 向量搜索

## HyDE 接口不变

`HyDEGenerator` 接口删除，`handleHyde` 直接持有 `*HyDEAPIClient`。
`HyDEModel` 接口（`Generate(ctx, prompt, maxTokens)`）也不再需要 —— `HyDEAPIClient.Generate(ctx, query)` 封装了 prompt 构造。

## config 变更

```go
type HyDEConfig struct {
    BaseURL   string `yaml:"base_url"`
    APIKey    string `yaml:"api_key"`
    Model     string `yaml:"model"`
    MaxTokens int    `yaml:"max_tokens"`
}
```

删除 `Enabled bool`。

## 不涉及

- Embedding 架构（本地 llama-go 不变）
- llama-go submodule
- Makefile / CGO 编译
- BM25 / 向量搜索 / Fusion

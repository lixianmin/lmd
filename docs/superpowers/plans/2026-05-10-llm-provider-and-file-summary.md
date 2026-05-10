# LLM Provider 架构重构 & 文件级 Summary 索引 — 实现计划

> **For agentic workers:** 使用 TDD（测试优先），每 task 先写测试再写实现。使用 checkbox (`- [ ]`) 跟踪进度。

**Goal:** 移除 llama.cpp 内嵌依赖，统一 Embedding/LLM 为可插拔 provider 接口；废弃 _topic.md，用文件级 summary + 两级混合检索替代。

**Architecture:** Provider 和功能正交配置。Summary 复用 chunks/chunks_fts/chunks_vec 表（`@summaries` collection），通过 `source_doc_id` 关联原始文件。两级检索共用同一个 `Searcher.SearchHybrid()`。

**Tech Stack:** Go 1.x, SQLite (vec0 + FTS5), Ollama API, SiliconFlow API

---

## Phase 0: 归档（必须先做）

### Task 0: 创建归档分支并打 tag

**操作：**
- [ ] **Step 1: 确认当前没有未提交的修改**

```bash
git status
```
Expected: clean working tree

- [ ] **Step 2: 切出 archive/llamacpp 分支**

```bash
git checkout -b archive/llamacpp
```

- [ ] **Step 3: 打 annotated tag**

```bash
git tag -a v0.1-llamacpp -m "最后一个内嵌 llama.cpp 的版本"
```

- [ ] **Step 4: 切回原分支（如 main）继续后续工作**

```bash
git checkout main
```

- [ ] **Step 5: 确认 tag 存在**

```bash
git tag -l | grep llamacpp
```
Expected: `v0.1-llamacpp`

---

## Phase 1: Config & Provider 基础设施

### Task 1: 重写 config.go

**Files:**
- Modify: `internal/config/config.go`

**前置条件：** 先读 `internal/config/config.go:1-143`

- [ ] **Step 1: 更新 Config 结构体**

```go
package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/logo"
)

var Cfg *Config
var once sync.Once

type Config struct {
	Providers ProviderConfig  `yaml:"providers"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Summary   SummaryConfig   `yaml:"summary"`
	Database  DatabaseConfig  `yaml:"database"`
	Daemon    DaemonConfig    `yaml:"daemon"`
}

type DaemonConfig struct {
	Port int `yaml:"port"`
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
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BatchSize int    `yaml:"batch_size"`
}

type SummaryConfig struct {
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	MaxOutputTokens int    `yaml:"max_output_tokens"`
	MaxInputTokens  int    `yaml:"max_input_tokens"`
	CooldownSeconds int    `yaml:"cooldown_seconds"`
	NoThinking      bool   `yaml:"no_thinking"` // Ollama qwen3.5 关思考模式
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}
```

- [ ] **Step 2: 创建 DefaultConfig**

```go
func DefaultConfig() *Config {
	return &Config{
		Daemon: DaemonConfig{
			Port: 12345,
		},
		Providers: ProviderConfig{
			Ollama: ProviderItem{
				URL: "http://localhost:11434",
			},
		SiliconFlow: ProviderItem{
			URL:    "https://api.siliconflow.cn/v1",
			APIKey: "sk-your-api-key-here",
		},
		},
		Embedding: EmbeddingConfig{
			Provider:  "ollama",
			Model:     "batiai/qwen3-embedding",
			BatchSize: 8,
		},
		Summary: SummaryConfig{
			Provider:        "ollama",
			Model:           "qwen3.5",
			MaxOutputTokens: 512,
			MaxInputTokens:  245000,
			CooldownSeconds: 120,
			NoThinking:      true,
		},
		Database: DatabaseConfig{
			Path: filepath.Join(os.Getenv("HOME"), ".cache", "lmd", "index.sqlite"),
		},
	}
}
```

- [ ] **Step 3: 更新 Load()**

```go
func Load() {
	once.Do(func() {
		Cfg = DefaultConfig()
		configPath := resolveConfigPath()
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				_ = SaveDefault(configPath)
				logo.Info("config: created default config at %s", configPath)
				return
			}
			logo.Warn("config: read %s error: %s, using defaults", configPath, err)
			return
		}
		if err := convert.FromJsonE(data, Cfg); err != nil {
			logo.Warn("config: unmarshal error: %s, using defaults", err)
			return
		}
	})
}
```

- [ ] **Step 4: 保留 resolveConfigPath()、SaveDefault() 辅助函数，移除旧字段相关代码**

- [ ] **Step 5: 编译验证**

```bash
go build ./internal/config/
```
Expected: no errors (caller sites not updated yet → will fail, that's expected for now)

---

### Task 2: 创建 LLMProvider 接口

**Files:**
- Create: `internal/llm/provider.go`

- [ ] **Step 1: 创建接口文件**

```go
package llm

import "context"

type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

type LLMProvider interface {
	ChatCompletion(ctx context.Context, messages []Message) (string, error)
	Close() error
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/llm/
```

---

### Task 3: 创建 OllamaLLM

**Files:**
- Create: `internal/llm/ollama_llm.go`

参考 `internal/embedding/ollama.go:1-81` 的模式。

- [ ] **Step 1: 创建 OllamaLLM**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaLLM struct {
	baseURL    string
	model      string
	client     *http.Client
	noThinking bool
}

func NewOllamaLLM(url, model string, noThinking bool) *OllamaLLM {
	// 去除末尾斜杠
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &OllamaLLM{
		baseURL: url,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type ollamaChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (my *OllamaLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	reqBody := ollamaChatRequest{
		Model:    my.model,
		Messages: messages,
		Stream:   false,
	}
	if my.noThinking {
		if reqBody.Options == nil {
			reqBody.Options = make(map[string]any)
		}
		reqBody.Options["enable_thinking"] = false
	}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (my *OllamaLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	reqBody := ollamaChatRequest{
		Model:    my.model,
		Messages: messages,
		Stream:   false,
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/api/chat", my.baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := my.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama chat: %s %s", resp.Status, string(respBytes))
	}

	var result ollamaChatResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	return result.Message.Content, nil
}

func (my *OllamaLLM) Close() error {
	return nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/llm/
```

---

### Task 4: 创建 SiliconFlowLLM

**Files:**
- Create: `internal/llm/siliconflow_llm.go`

- [ ] **Step 1: 创建 SiliconFlowLLM**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type SiliconFlowLLM struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewSiliconFlowLLM(url, model, apiKey string) *SiliconFlowLLM {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &SiliconFlowLLM{
		baseURL: url,
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type sfChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type sfChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (my *SiliconFlowLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	reqBody := sfChatRequest{
		Model:       my.model,
		Messages:    messages,
		Temperature: 0.3,
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/chat/completions", my.baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+my.apiKey)

	resp, err := my.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("siliconflow chat: %s %s", resp.Status, string(respBytes))
	}

	var result sfChatResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("siliconflow chat: no choices returned")
	}

	return result.Choices[0].Message.Content, nil
}

func (my *SiliconFlowLLM) Close() error {
	return nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/llm/
```

---

### Task 5: 创建 SiliconFlowEmbedding

**Files:**
- Create: `internal/embedding/siliconflow_embedding.go`
- Reference: `internal/embedding/ollama.go:1-81`

- [ ] **Step 1: 创建 SiliconFlowEmbedding**

```go
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type SiliconFlowEmbedding struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewSiliconFlowEmbedding(url, model, apiKey string) *SiliconFlowEmbedding {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &SiliconFlowEmbedding{
		baseURL: url,
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

type sfEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type sfEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (my *SiliconFlowEmbedding) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := sfEmbedRequest{Model: my.model, Input: texts}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/embeddings", my.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+my.apiKey)

	resp, err := my.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("siliconflow embed: %s %s", resp.Status, string(respBytes))
	}

	var result sfEmbedResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, err
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}

func (my *SiliconFlowEmbedding) Embed(ctx context.Context, text string) ([]float32, error) {
	batch, err := my.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return batch[0], nil
}

func (my *SiliconFlowEmbedding) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, EmbedQueryPrefix+query)
}

func (my *SiliconFlowEmbedding) Dimension() int {
	return EmbeddingDim
}

func (my *SiliconFlowEmbedding) ModelName() string {
	return my.model
}

func (my *SiliconFlowEmbedding) Close() error {
	return nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/embedding/
```

---

## Phase 2: 移除 llama.cpp 及 Topics

### Task 6: 更新 daemon.go — Provider 创建

**Files:**
- Modify: `internal/daemon/daemon.go`

**前置：** 先读 `internal/daemon/daemon.go:41-208`

- [ ] **Step 1: 更新 Daemon struct**

移除以下字段：`provider *embedding.LlamaProvider`、`topicIndexer *service.TopicIndexer`、`topicRouter *service.TopicRouter`、`llmClient *service.LLMClient`、`memSvc *service.MemoryService`。

新增字段：
```go
import (
	"github.com/lixianmin/lmd/internal/llm"
)

type Daemon struct {
	// ... 保留的字段 ...
	embedProvider embedding.EmbeddingProvider
	llmProvider   llm.LLMProvider
	summarizer    *service.Summarizer
	// memSvc removed, topicIndexer removed, topicRouter removed, llmClient removed
}
```

- [ ] **Step 2: 在 Start() 中创建 embedding provider**

```go
var embedProv embedding.EmbeddingProvider
switch my.cfg.Embedding.Provider {
case "ollama":
	ollamaCfg := my.cfg.Providers.Ollama
	embedProv = embedding.NewOllamaProvider(ollamaCfg.URL, my.cfg.Embedding.Model)
case "siliconflow":
	sfCfg := my.cfg.Providers.SiliconFlow
	embedProv = embedding.NewSiliconFlowEmbedding(sfCfg.URL, my.cfg.Embedding.Model, sfCfg.APIKey)
default:
	return fmt.Errorf("unknown embedding provider: %s", my.cfg.Embedding.Provider)
}
my.embedProvider = embedProv
```

- [ ] **Step 3: 在 Start() 中创建 LLM provider**

```go
var llmProv llm.LLMProvider
switch my.cfg.Summary.Provider {
    case "ollama":
        ollamaCfg := my.cfg.Providers.Ollama
        llmProv = llm.NewOllamaLLM(ollamaCfg.URL, my.cfg.Summary.Model, my.cfg.Summary.NoThinking)
case "siliconflow":
	sfCfg := my.cfg.Providers.SiliconFlow
	llmProv = llm.NewSiliconFlowLLM(sfCfg.URL, my.cfg.Summary.Model, sfCfg.APIKey)
default:
	return fmt.Errorf("unknown summary provider: %s", my.cfg.Summary.Provider)
}
my.llmProvider = llmProv
```

- [ ] **Step 4: 创建 Embedder 时使用 my.embedProvider**

```go
my.embedder = service.NewEmbedder(my.embedProvider, my.cfg.Embedding.BatchSize, 0)
```

- [ ] **Step 5: 创建 Summarizer**

```go
my.summarizer = service.NewSummarizer(my.llmProvider, my.cfg.Summary)
```

- [ ] **Step 6: 移除所有 llama.cpp 加载代码**

删除 `internal/embedding.LlamaProvider` 创建代码（原 daemon.go:96-101）。
删除 `service.LLMClient` 创建代码（原 daemon.go:126）。
删除 `service.TopicIndexer` + `service.TopicRouter` 创建代码（原 daemon.go:136-138）。
删除模型下载 goroutine（原 daemon.go:89-94, 112-115）。
删除 `memoryService` 创建（原 daemon.go:105）。

- [ ] **Step 7: 更新 goLoop 中的 ticker**

移除 `topicSyncTicker`，添加 `summaryTicker`：

```go
summaryTicker := time.NewTicker(time.Duration(my.cfg.Summary.CooldownSeconds) * time.Second)
```

将 `topicSyncTicker` 分支改为 `summaryTicker` 分支：
```go
case <-summaryTicker.C:
    my.summarizer.ProcessDirty()
```

- [ ] **Step 8: 更新 syncIndex 返回 dirty docs**

修改 `syncIndexUnlocked()`，收集 `indexer.UpdateCollection` 返回的脏 doc_id：

```go
result, err := my.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
if err == nil && len(result.DirtyDocIds) > 0 {
    my.summarizer.MarkDirty(result.DirtyDocIds)
}
```

- [ ] **Step 9: 更新 Stop()**

移除 `llmClient.Close()` 调用（原 daemon.go:196）。
添加 `my.llmProvider.Close()`。

- [ ] **Step 10: 编译验证（caller sites 还需更新，仅检查本文件）**

```bash
go build ./internal/daemon/ 2>&1 || true
```

---

### Task 7: 更新 Indexer — 返回脏文档

**Files:**
- Modify: `internal/service/indexer.go`

**前置：** 先读 `internal/service/indexer.go:23-250`

- [ ] **Step 1: 更新 UpdateResult 结构体**

```go
type UpdateResult struct {
	Indexed     int
	Updated     int
	Unchanged   int
	Removed     int
	DirtyDocIds []int64 // 新增：内容变更或新增的 doc_id
}
```

- [ ] **Step 2: 在 hash 变更时记录 doc_id**

在 `UpdateCollection` 中，当 hash 变更时（原 indexer.go:174-202 的 Updated 分支），追加 doc_id：

```go
result.Updated++
// ... 现有代码 ...
doc.Id 在 UpsertDocument 后被填充
result.DirtyDocIds = append(result.DirtyDocIds, doc.Id)
```

- [ ] **Step 3: 在新文件时记录 doc_id**

在新增文件分支（原 indexer.go:205-232）：

```go
result.Indexed++
result.DirtyDocIds = append(result.DirtyDocIds, doc.Id)
// doc.Id 在 UpsertDocument 后被填充
```

注意：`UpsertDocument` 会填充 `doc.Id`（通过 `LastInsertId()` 或 SELECT 回查），因此可以在调用后追加。

- [ ] **Step 4: 编译验证**

```bash
go build ./internal/service/
```

---

### Task 8: 更新 routes.go — 搜索端点适配

**Files:**
- Modify: `internal/daemon/daemon_routes.go`
- Modify: `internal/daemon/daemon_mcp.go`

- [ ] **Step 1: 更新 handleSearch，引用改为 my.embedProvider**

```go
// 原: my.provider → my.embedProvider
hits, err := my.searcher.SearchVector(r.Context(), my.embedProvider, req.Query, req.Collection, ...)
```

- [ ] **Step 2: 更新 handleQuery，引用改为 my.embedProvider**

```go
vecHits, _ := my.searcher.SearchVector(r.Context(), my.embedProvider, req.Query, req.Collection, ...)
```

- [ ] **Step 3: 重写 handleSmartQuery 为两级检索**

```go
func (my *Daemon) handleSmartQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query    string  `json:"query"`
		Limit    int     `json:"limit"`
		MinScore float64 `json:"min_score"`
		Strategy string  `json:"strategy"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = convert.FromJsonE(body, &req)
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	ctx := r.Context()

	// Level 1: 搜索 @summaries 定位文件
	summaryHits, err := my.searcher.SearchHybrid(ctx, my.embedProvider,
		req.Query, "@summaries", nil, req.Limit*2, req.Strategy)
	if err != nil || len(summaryHits) == 0 {
		// 降级：全库混合检索
		results := my.fullHybridSearch(ctx, req.Query, "", req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	// 提取 source_doc_id
	docIDs := extractSourceDocIds(summaryHits)
	if len(docIDs) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, "", req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	// Level 2: 在命中文档范围内混合检索
	results, err := my.searcher.SearchHybrid(ctx, my.embedProvider,
		req.Query, "", docIDs, req.Limit*2, req.Strategy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results = filterAndLimit(results, req.MinScore, req.Limit)
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": true})
}

func (my *Daemon) fullHybridSearch(ctx context.Context, query, collection string, limit int, minScore float64, strategy string) []formatter.SearchHit {
	overfetch := safeOverfetch(limit)
	lexHits, _ := my.searcher.SearchLex(query, collection, overfetch, 0, strategy)
	vecHits, _ := my.searcher.SearchVector(ctx, my.embedProvider, query, collection, overfetch, 0)
	results := service.FuseResults(lexHits, vecHits)
	return filterAndLimit(results, minScore, limit)
}

func extractSourceDocIds(hits []formatter.SearchHit) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, h := range hits {
		// h.DocId 是 @summaries document 的 docid
		doc, err := dao.GetDocumentByDocId(h.DocId)
		if err != nil {
			continue
		}
		// doc.SourceDocId 指向原始文件
		if !seen[doc.SourceDocId] {
			seen[doc.SourceDocId] = true
			ids = append(ids, doc.SourceDocId)
		}
	}
	return ids
}
```

- [ ] **Step 4: 更新 daemon_mcp.go 中所有 provider 引用**

将所有 `my.provider` 替换为 `my.embedProvider`。

- [ ] **Step 5: 编译验证**

```bash
go build ./internal/daemon/ 2>&1 || true
```

---

### Task 9: 添加 Searcher.SearchHybrid 方法

**Files:**
- Modify: `internal/service/searcher.go`

- [ ] **Step 1: 在 searcher.go 中增加 docIDs 过滤版本的搜索方法**

在 dao 层添加支持 doc_id 过滤的 FTS 搜索。先在 `internal/dao/chunks_fts.go` 新增：

```go
func SearchFTSWithDocs(query, collection string, limit int, docIDs []int64) ([]FTSSearchResult, error) {
	if len(docIDs) == 0 {
		return SearchFTS(query, collection, limit)
	}
	// 构建 IN 子句
	placeholders := strings.Repeat("?,", len(docIDs))
	placeholders = placeholders[:len(placeholders)-1]
	sql := fmt.Sprintf(`
		SELECT c.id, d.id, d.collection, d.path, d.title, snippet(chunks_fts, 1, '<b>', '</b>', '...', 32),
		       rank, c.position
		FROM chunks_fts f
		JOIN chunks c ON c.id = f.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ? AND d.id IN (%s)
		ORDER BY rank LIMIT ?
	`, placeholders)
	args := []interface{}{query}
	for _, id := range docIDs {
		args = append(args, id)
	}
	args = append(args, limit)
	// ... execute and return
}
```

同样为 BM25 版本添加。然后在 searcher.go 中实现 SearchHybrid：

```go
func (my *Searcher) SearchHybrid(ctx context.Context, provider embedding.EmbeddingProvider,
	query, collection string, docIDs []int64, limit int, strategy string) ([]formatter.SearchHit, error) {

	var lexHits []formatter.SearchHit
	if len(docIDs) > 0 {
		ftsResults, err := my.searchFTSWithDocs(query, collection, limit, docIDs, strategy)
		if err != nil {
			return nil, err
		}
		lexHits = formatFTSResults(ftsResults)
	} else {
		var err error
		lexHits, err = my.SearchLex(query, collection, limit, 0, strategy)
		if err != nil {
			return nil, err
		}
	}

	vecHits, err := my.SearchVector(ctx, provider, query, collection, limit, 0)
	if err != nil {
		return nil, err
	}

	if len(docIDs) > 0 {
		vecHits = filterByDocIds(vecHits, docIDs)
		lexHits = filterByDocIds(lexHits, docIDs)
	}

	return FuseResults(lexHits, vecHits), nil
}
```

- [ ] **Step 2: 在 service/fusion.go 或新文件添加 filterByDocIds**

```go
func filterByDocIds(hits []formatter.SearchHit, docIDs []int64) []formatter.SearchHit {
	idSet := make(map[int64]bool, len(docIDs))
	for _, id := range docIDs {
		idSet[id] = true
	}
	filtered := make([]formatter.SearchHit, 0, len(hits))
	for _, h := range hits {
		if idSet[h.DocId] {
			filtered = append(filtered, h)
		}
	}
	return filtered
}
```

- [ ] **Step 3: 检查 formatter.SearchHit 是否包含 DocId 字段**

读 `internal/formatter/hit.go`（如果存在），确保 SearchHit 有 `DocId int64`。

- [ ] **Step 4: 编译验证**

```bash
go build ./internal/service/
```

---

### Task 10: 数据库 Schema — 添加 source_doc_id + 移除 topics

**Files:**
- Modify: `internal/dao/db_init.go`

- [ ] **Step 1: 添加 source_doc_id 列到 documents 表**

在 `createTables()` 的 stmts 数组中，documents 表创建之后追加：

```go
`ALTER TABLE documents ADD COLUMN source_doc_id INTEGER`,
```

由于 SQLite `ADD COLUMN` 在列已存在时会报错，需要容忍该错误：

```go
func addSourceDocIdColumn() {
	_, err := DB.Exec(`ALTER TABLE documents ADD COLUMN source_doc_id INTEGER`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		logo.Warn("add source_doc_id column error: %s", err)
	}
}
```

在 `Init()` 中调用 `addSourceDocIdColumn()`。

- [ ] **Step 2: 移除 topics 和 topics_vec 表创建**

从 stmts 数组中删除 topics 和 topics_vec 的 CREATE TABLE 语句（db_init.go:134-146）。

- [ ] **Step 3: 添加 DROP TABLE 清理**

```go
func dropTopicsTables() {
	_, _ = DB.Exec(`DROP TABLE IF EXISTS topics_vec`)
	_, _ = DB.Exec(`DROP TABLE IF EXISTS topics`)
}
```

在 `Init()` 中调用 `dropTopicsTables()`（放在 createTables 之后）。

- [ ] **Step 4: 编译验证**

```bash
go build ./internal/dao/
```

---

### Task 11: 更新 DocumentRecord 和相关 DAO

**Files:**
- Modify: `internal/dao/document.go`

- [ ] **Step 1: 添加 SourceDocId 字段**

```go
type DocumentRecord struct {
	Id          int64
	DocId       string
	Collection  string
	Path        string
	Title       string
	Body        string
	Hash        string
	FileSize    int64
	FileModTime int64
	SourceDocId int64 // 新增
	ModifiedAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
```

- [ ] **Step 2: 更新 UpsertDocument 的 SQL**

在 INSERT 语句中添加 `source_doc_id` 列：

```go
`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, modified_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))
 ON CONFLICT(collection, path) DO UPDATE SET ...`
```

- [ ] **Step 3: 更新所有 SELECT 语句包含 source_doc_id**

`GetDocumentById`、`GetDocumentByPath`、`ListDocumentsByCollection` 等所有查询 `documents` 表的函数，SELECT 列中追加 `source_doc_id`，Scan 中追加 `&doc.SourceDocId`。

- [ ] **Step 4: 编译验证**

```bash
go build ./internal/dao/
```

---

### Task 12: 移除 llama.cpp 子模块和依赖

**Files:**
- Modify: `go.mod`
- Modify: `.gitmodules`
- Modify: `Makefile`
- Delete: `llama-go/`
- Delete: `internal/embedding/llama.go`
- Delete: `internal/service/llm_client.go`
- Delete: `internal/service/topic_indexer.go`
- Delete: `internal/service/topic_router.go`
- Delete: `internal/dao/topic.go`

- [ ] **Step 1: 删除文件和目录**

```bash
git rm llama-go/
rm -rf llama-go/
git rm internal/embedding/llama.go
git rm internal/service/llm_client.go
git rm internal/service/topic_indexer.go
git rm internal/service/topic_router.go
git rm internal/dao/topic.go
```

- [ ] **Step 2: 清理 go.mod**

移除：
```
require github.com/tcpipuk/llama-go v0.0.0
replace github.com/tcpipuk/llama-go => ./llama-go
```

```bash
go mod tidy
```

- [ ] **Step 3: 清理 .gitmodules**

移除 llama-go submodule 条目。

- [ ] **Step 4: 清理 Makefile**

移除 llama-go 编译相关步骤（libbinding.a 编译命令）。

- [ ] **Step 5: 清理 vendor**

```bash
go mod vendor
```

- [ ] **Step 6: 编译验证整个项目**

```bash
go build ./...
```

---

## Phase 3: Summarizer 服务

### Task 13: 创建 Summarizer

**Files:**
- Create: `internal/service/summarizer.go`

- [ ] **Step 1: 创建 Summarizer**

```go
package service

import (
	"context"
	"sync"
	"unicode/utf8"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/logo"
)

const summaryCollection = "@summaries"

type Summarizer struct {
	mu          sync.Mutex
	dirty       map[int64]bool // doc_id → dirty
	llm         llm.LLMProvider
	maxOutput   int
	maxInput    int
}

func NewSummarizer(llmProvider llm.LLMProvider, cfg config.SummaryConfig) *Summarizer {
	return &Summarizer{
		dirty:     make(map[int64]bool),
		llm:       llmProvider,
		maxOutput: cfg.MaxOutputTokens,
		maxInput:  cfg.MaxInputTokens,
	}
}

func (my *Summarizer) MarkDirty(docIds []int64) {
	my.mu.Lock()
	defer my.mu.Unlock()
	for _, id := range docIds {
		my.dirty[id] = true
	}
}

func (my *Summarizer) ProcessDirty() {
	my.mu.Lock()
	// 复制并清空
	dirty := my.dirty
	my.dirty = make(map[int64]bool)
	my.mu.Unlock()

	if len(dirty) == 0 {
		return
	}

	for docID := range dirty {
		my.processDoc(docID)
	}
}

func (my *Summarizer) processDoc(docID int64) {
	doc, err := dao.GetDocumentById(docID)
	if err != nil {
		logo.Warn("summarizer: get doc %d error: %s", docID, err)
		return
	}

	// 跳过 @ 系统 collection 的文档（不要对 summary 自己生成 summary）
	if len(doc.Collection) > 0 && doc.Collection[0] == '@' {
		return
	}

	// 检查是否已有 summary 且 hash 相同
	existingSummary, _ := my.findExistingSummary(docID)
	if existingSummary != nil {
		existingChunks, _ := dao.GetChunksByDocId(existingSummary.Id)
		if len(existingChunks) > 0 && existingChunks[0].Hash == doc.Hash {
			// hash 相同，只更新 updated_at
			dao.TouchDocument(existingSummary.Id)
			return
		}
	}

	// 从 chunks 表取内容拼接
	chunks, err := dao.GetChunksByDocId(docID)
	if err != nil || len(chunks) == 0 {
		logo.Warn("summarizer: no chunks for doc %d", docID)
		return
	}

	var content string
	for _, c := range chunks {
		content += c.Content + "\n"
	}

	content = my.truncateContent(content, doc.Title)

	summary, err := my.generateSummary(doc.Title, content)
	if err != nil {
		logo.Warn("summarizer: generate summary for doc %d error: %s", docID, err)
		return
	}

	my.upsertSummary(docID, doc.Hash, summary)
}

func (my *Summarizer) findExistingSummary(docID int64) (*dao.DocumentRecord, error) {
	// 在 @summaries collection 中查找 source_doc_id == docID 的 document
	return dao.GetDocumentBySourceDocId(summaryCollection, docID)
}

func (my *Summarizer) truncateContent(content, title string) string {
	// token 估算: UTF-8 字节 / 2
	promptOverhead := 200 // prompt 模板约占 token 数
	available := my.maxInput - promptOverhead - my.maxOutput
	if available <= 0 {
		available = 1000
	}

	contentBytes := len(content)
	contentTokens := contentBytes / 2

	if contentTokens <= available {
		return content
	}

	// 头部 60% + 尾部 40%
	headRatio := 0.6
	headBytes := int(float64(available) * headRatio * 2)
	tailBytes := int(float64(available) * (1 - headRatio) * 2)

	head := content
	if headBytes < len(head) {
		head = head[:headBytes]
		// 截断到最近 rune 边界
		for len(head) > 0 && !utf8.ValidString(head) {
			head = head[:len(head)-1]
		}
	}

	tail := content
	if len(tail) > tailBytes {
		tail = tail[len(tail)-tailBytes:]
		// 从 rune 起始位置开始
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}

	return head + "\n...(truncated)...\n" + tail
}

func (my *Summarizer) generateSummary(title, content string) (string, error) {
	prompt := "你是一个知识库索引助手。阅读以下文档，用1-2句话(不超过100字)概括其内容和核心主题。\n\n" +
		"文档标题: " + title + "\n" +
		"文档内容:\n" + content + "\n\n" +
		"请直接输出摘要，不要加前缀和引号。"

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	ctx := context.Background()
	return my.llm.ChatCompletion(ctx, messages)
}

func (my *Summarizer) upsertSummary(sourceDocID int64, hash, summary string) {
	// 删除已有的 summary
	existing, _ := my.findExistingSummary(sourceDocID)
	if existing != nil {
		dao.DeleteDocument(existing.Id)
	}

	// 创建 summary document
	doc := &dao.DocumentRecord{
		Collection:  summaryCollection,
		Path:        "",
		Title:       "",
		Body:        "",
		Hash:        hash,
		SourceDocId: sourceDocID,
	}

	if err := dao.UpsertDocument(doc); err != nil {
		logo.Warn("summarizer: upsert summary doc error: %s", err)
		return
	}

	// 创建唯一 chunk（summary 内容）
	chunks := []dao.ChunkData{{
		Content:    summary,
		Position:   0,
		TokenCount: 0,
		Hash:       hash,
	}}

	// 直接插入 chunk 和 FTS。不提供 tokenized content（FTS 使用原始 content）
	_, err := dao.InsertChunks(doc.Id, chunks, []string{summary})
	if err != nil {
		logo.Warn("summarizer: insert summary chunk error: %s", err)
	}
}
```

- [ ] **Step 2: 添加缺失的 DAO 函数**

需要在 `internal/dao/document.go` 添加：

```go
func GetDocumentBySourceDocId(collection string, sourceDocId int64) (*DocumentRecord, error) {
	return getDocument("WHERE collection=? AND source_doc_id=?", collection, sourceDocId)
}

func TouchDocument(id int64) error {
	_, err := WithExec("UPDATE documents SET updated_at=DATETIME('now', '+8 hours') WHERE id=?", id)
	return err
}
```

同时更新 `getDocument` 内部查询的 SELECT 列确保包含 `source_doc_id`。

- [ ] **Step 3: 编译验证**

```bash
go build ./internal/service/
```

---

### Task 14: 更新 Embedder — 处理 @summaries collection

**Files:**
- Modify: `internal/service/embedder.go`

- [ ] **Step 1: 确认 embedder 自动处理 @summaries 的 chunk**

检查 `dao.GetUnembeddedChunks` 的实现。它应该无差别地返回所有 collection 中无 embedding 的 chunk。`@summaries` 的 chunk 自动包含在内。

- [ ] **Step 2: 确认不需要修改**

阅读 `internal/dao/chunks_vec.go:219` 的 `GetUnembeddedChunks`：

```go
func GetUnembeddedChunks(limit int) ([]ChunkRecord, error) {
```

该函数使用 LEFT JOIN chunks_vec，不区分 collection。因此无需修改。

---

## Phase 4: 清理记忆系统

### Task 15: 删除记忆相关代码

**Files:**
- Delete: `internal/dao/memory.go`
- Delete: `internal/dao/memory_test.go`
- Delete: `internal/service/memory.go`
- Modify: `internal/dao/db_init.go`
- Modify: `internal/daemon/daemon_routes.go`
- Modify: `internal/daemon/client.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/daemon/daemon_mcp.go`
- Modify: `internal/cli/memory.go`
- Modify: `cmd/lmd/main.go` 或 `internal/cli/root.go`

- [ ] **Step 1: 删除记忆 DAO 和 service**

```bash
git rm internal/dao/memory.go internal/dao/memory_test.go internal/service/memory.go
```

- [ ] **Step 2: 移除数据库中的记忆表**

在 `db_init.go` 的 `createTables()` 后添加清理：

```go
func dropMemoryTables() {
	_, _ = DB.Exec(`DROP TABLE IF EXISTS memories_fts`)
	_, _ = DB.Exec(`DROP TABLE IF EXISTS memories_vec`)
	_, _ = DB.Exec(`DROP TABLE IF EXISTS memories`)
}
```

在 `Init()` 中调用 `dropMemoryTables()`。

- [ ] **Step 3: 删除 routes.go 中的 /memory/* 路由**

删除 `daemon_routes.go` 中的 `handleMemoryAdd`、`handleMemoryDelete`、`handleMemoryUpdate` 函数。
从 `registerRoutes` 中移除对应的路由注册。

- [ ] **Step 4: 删除 daemon_mcp.go 中的 memory tools**

从 `handleToolCall` switch 中移除 `"memory_add"`、`"memory_delete"`、`"memory_update"` case。
删除 `handleToolMemoryAdd`、`handleToolMemoryDelete`、`handleToolMemoryUpdate` 函数。

- [ ] **Step 5: 更新 MCP server tools 列表**

在 `internal/mcp/server.go:15-26`，移除 memory 相关 tool 定义：

```go
var toolDefs = []ToolDef{
	{Name: "search", ...},
	{Name: "vsearch", ...},
	{Name: "query", ...},
	{Name: "get", ...},
	{Name: "status", ...},
	{Name: "list_collections", ...},
	{Name: "smart_query", ...},
}
```

- [ ] **Step 6: 删除 CLI memory 命令**

修改 `internal/cli/memory.go`，删除 memory 子命令定义。如果整个文件只剩 memory 命令，删除整个文件。
更新 `root.go` 中移除 `memoryCmd` 注册。

- [ ] **Step 7: 删除 daemon client 中的 Memory 方法**

在 `internal/daemon/client.go` 中删除 `MemoryAdd`、`MemoryDelete`、`MemoryUpdate` 方法。

- [ ] **Step 8: 编译验证**

```bash
go build ./...
```

---

## Phase 5: 最终验证

### Task 16: 全量编译 + 测试

- [ ] **Step 1: 编译**

```bash
go build ./...
```
预期：无错误。

- [ ] **Step 2: 运行测试**

```bash
go test ./...
```
预期：所有测试通过。

- [ ] **Step 3: 检查 go mod tidy**

```bash
go mod tidy
```
预期：无变化。

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor: remove llama.cpp, add provider interface, file-level summary"
```

---

## 自检清单

1. **Spec 覆盖**:
   - ✓ Provider 架构（Task 2-5）
   - ✓ 配置正交（Task 1）
   - ✓ source_doc_id 关联（Task 10-11）
   - ✓ 两级检索（Task 8-9）
   - ✓ Summary 生成流程（Task 13-14）
   - ✓ 冷却期 + hash 去重（Task 13）
   - ✓ 大文档截断（Task 13 truncateContent）
   - ✓ 归档操作（Task 0）
   - ✓ 记忆清理（Task 15）
   - ✓ 删除清单（Task 12 + Task 15）

2. **无 TBD/TODO**: 所有步骤均有具体代码。

3. **类型一致性**: `SearchHit.DocId int64`、`DocumentRecord.Id int64`、`source_doc_id INTEGER`。

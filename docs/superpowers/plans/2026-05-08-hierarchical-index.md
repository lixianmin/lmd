# 层级索引系统 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每个目录自动生成 `_topic.md` 摘要，查询时通过 embedding 向量路由定位到 Top-3 目录，再在目录内做混合检索。

**Architecture:** 3 层 — DAO（topics 表 + topics_vec 向量表）、Service（TopicIndexer 离线摘要生成、TopicRouter 在线路由）、Daemon（后台定时 + HTTP 端点）。`_topic.md` 是唯一真相源，SQLite 是派生缓存。路由复用现有 embedding 模型，摘要用 Qwen3-4B-Instruct。

**Tech Stack:** Go, SQLite + sqlite-vec, llama-go (CGo), Qwen3-4B-Instruct-2507 GGUF

---

## File Structure

| File | New/Modify | Responsibility |
|------|-----------|----------------|
| `internal/config/config.go` | Modify | 新增 TopicConfig |
| `internal/dao/topic.go` | Create | topics 表 CRUD + topics_vec 向量操作 |
| `internal/dao/topic_test.go` | Create | DAO 测试 |
| `internal/service/llm_client.go` | Create | llama-go 加载 Qwen3-4B-Instruct 生成文本 |
| `internal/service/llm_client_test.go` | Create | LLM 客户端测试 |
| `internal/service/topic_indexer.go` | Create | 遍历目录 → LLM 生成 _topic.md → 解析 → 写入 DB |
| `internal/service/topic_indexer_test.go` | Create | 索引器测试 |
| `internal/service/topic_router.go` | Create | 查询路由：embed → vector match topics → 搜索 |
| `internal/service/topic_router_test.go` | Create | 路由器测试 |
| `internal/dao/db_init.go` | Modify | 新增 topics 表和 topics_vec 虚拟表 |
| `internal/daemon/daemon.go` | Modify | 新增 topicSyncTicker 后台定时 |
| `internal/daemon/daemon_routes.go` | Modify | 新增 handleSmartQuery HTTP 端点 |
| `internal/daemon/daemon_mcp.go` | Modify | 新增 smart_query MCP 工具 |
| `internal/mcp/server.go` | Modify | toolDefs 追加 smart_query |

---

### Task 1: Config — TopicConfig

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add TopicConfig struct and wire into Config**

In `internal/config/config.go`, add after `DatabaseConfig` type:

```go
type TopicConfig struct {
	SummarizeModel      string `yaml:"summarize_model"`
	SummarizeGPULayers  int    `yaml:"summarize_gpu_layers"`
	SummarizeThreads    int    `yaml:"summarize_threads"`
	CooldownSeconds     int    `yaml:"cooldown_seconds"`
}
```

Add `Topic TopicConfig `yaml:"topic"`` into the `Config` struct after `Database`:

```go
type Config struct {
	Daemon    DaemonConfig    `yaml:"daemon"`
	Llama     LlamaConfig     `yaml:"llama"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	HyDE      HyDEConfig      `yaml:"hyde"`
	Vector    VectorConfig    `yaml:"vector"`
	Database  DatabaseConfig  `yaml:"database"`
	Topic     TopicConfig     `yaml:"topic"`
}
```

- [ ] **Step 2: Set defaults in initDefaults()**

In `config.go`, find `initDefaults()` and add Topic defaults before the closing brace:

```go
if Cfg.Topic.SummarizeModel == "" {
	Cfg.Topic.SummarizeModel = filepath.Join(home, ".cache", "lmd", "models", "Qwen3-4B-Instruct-2507-Q4_K_M.gguf")
}
if Cfg.Topic.SummarizeGPULayers == 0 {
	Cfg.Topic.SummarizeGPULayers = -1
}
if Cfg.Topic.SummarizeThreads == 0 {
	Cfg.Topic.SummarizeThreads = 4
}
if Cfg.Topic.CooldownSeconds == 0 {
	Cfg.Topic.CooldownSeconds = 300
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/config/`
Expected: compiles successfully

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: add TopicConfig for hierarchical index"
```

---

### Task 2: DAO — topics table + topics_vec

**Files:**
- Create: `internal/dao/topic.go`
- Create: `internal/dao/topic_test.go`
- Modify: `internal/dao/db_init.go`

**Spec reference:** Section 4 (Schema 变更)

- [ ] **Step 1: Write failing test for UpsertTopic**

Create `internal/dao/topic_test.go`:

```go
package dao

import (
	"encoding/json"
	"testing"
)

func TestUpsertTopic(t *testing.T) {
	setupTestDB(t)

	docPaths := []string{"mysql-index.md", "query-plan.md", "btree-hash.md"}
	docPathsJSON, _ := json.Marshal(docPaths)

	err := UpsertTopic("test-coll", "db/", "数据库优化相关资料综述", string(docPathsJSON), "abc123")
	if err != nil {
		t.Fatalf("UpsertTopic failed: %v", err)
	}

	topic, err := GetTopic("test-coll", "db/")
	if err != nil {
		t.Fatalf("GetTopic failed: %v", err)
	}
	if topic.Overview != "数据库优化相关资料综述" {
		t.Errorf("overview mismatch: got %q", topic.Overview)
	}

	var paths []string
	json.Unmarshal([]byte(topic.DocPaths), &paths)
	if len(paths) != 3 {
		t.Errorf("doc_paths len: want 3, got %d", len(paths))
	}
	if topic.Hash != "abc123" {
		t.Errorf("hash mismatch: got %q", topic.Hash)
	}
}

func TestListTopicsByCollection(t *testing.T) {
	setupTestDB(t)

	_ = UpsertTopic("test-coll", "", "root overview", `["a.md"]`, "h1")
	_ = UpsertTopic("test-coll", "sub/", "sub overview", `["b.md"]`, "h2")

	topics, err := ListTopicsByCollection("test-coll")
	if err != nil {
		t.Fatalf("ListTopicsByCollection failed: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("want 2 topics, got %d", len(topics))
	}
}

func TestUpsertTopicOverwrite(t *testing.T) {
	setupTestDB(t)

	_ = UpsertTopic("test-coll", "db/", "old overview", `["a.md"]`, "h1")
	_ = UpsertTopic("test-coll", "db/", "new overview", `["a.md","b.md"]`, "h2")

	topic, _ := GetTopic("test-coll", "db/")
	if topic.Overview != "new overview" {
		t.Errorf("overview not updated: got %q", topic.Overview)
	}
	if topic.Hash != "h2" {
		t.Errorf("hash not updated: got %q", topic.Hash)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dao/ -run TestUpsertTopic -v`
Expected: FAIL - `undefined: UpsertTopic`

- [ ] **Step 3: Add tables to db_init.go**

In `internal/dao/db_init.go`, add to `createTables()` `stmts` slice after the last index entry:

```go
`CREATE TABLE IF NOT EXISTS topics (
    collection  TEXT NOT NULL,
    rel_path    TEXT NOT NULL,
    overview    TEXT NOT NULL,
    doc_paths   TEXT NOT NULL,
    hash        TEXT NOT NULL,
    updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
    PRIMARY KEY (collection, rel_path)
)`,
`CREATE VIRTUAL TABLE IF NOT EXISTS topics_vec USING vec0(
    topic_rowid INTEGER PRIMARY KEY,
    overview_vector float[1024] distance_metric=cosine
)`,
```

- [ ] **Step 4: Write topic.go implementation**

Create `internal/dao/topic.go`:

```go
package dao

import (
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type TopicRecord struct {
	Collection string
	RelPath    string
	Overview   string
	DocPaths   string
	Hash       string
	UpdatedAt  string
}

func UpsertTopic(collection, relPath, overview, docPaths, hash string) error {
	_, err := WithExec(`
		INSERT INTO topics (collection, rel_path, overview, doc_paths, hash)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(collection, rel_path) DO UPDATE SET
			overview=excluded.overview,
			doc_paths=excluded.doc_paths,
			hash=excluded.hash,
			updated_at=DATETIME('now', '+8 hours')
	`, collection, relPath, overview, docPaths, hash)
	return err
}

func GetTopic(collection, relPath string) (*TopicRecord, error) {
	var t TopicRecord
	err := withQueryRow(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE collection=? AND rel_path=?",
		collection, relPath,
	).Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("topic not found: %s/%s: %w", collection, relPath, err)
	}
	return &t, nil
}

func ListTopicsByCollection(collection string) ([]TopicRecord, error) {
	rows, err := withQuery(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE collection=? ORDER BY rel_path",
		collection,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicRecord
	for rows.Next() {
		var t TopicRecord
		if err := rows.Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func ListAllTopics() ([]TopicRecord, error) {
	rows, err := withQuery(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics ORDER BY collection, rel_path",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicRecord
	for rows.Next() {
		var t TopicRecord
		if err := rows.Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func DeleteTopic(collection, relPath string) error {
	_, err := WithExec("DELETE FROM topics WHERE collection=? AND rel_path=?", collection, relPath)
	return err
}

func DeleteTopicsByCollection(collection string) error {
	_, err := WithExec("DELETE FROM topics WHERE collection=?", collection)
	return err
}

func GetTopicRowID(collection, relPath string) (int64, error) {
	var rowID int64
	err := withQueryRow(
		"SELECT rowid FROM topics WHERE collection=? AND rel_path=?",
		collection, relPath,
	).Scan(&rowID)
	return rowID, err
}

func GetTopicByRowID(rowID int64) (*TopicRecord, error) {
	var t TopicRecord
	err := withQueryRow(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE rowid=?",
		rowID,
	).Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("topic not found for rowid %d: %w", rowID, err)
	}
	return &t, nil
}

func UpsertTopicVector(topicRowID int64, embedding []float32) error {
	padded := padVector(embedding)
	vec, err := sqlite_vec.SerializeFloat32(padded)
	if err != nil {
		return err
	}
	_, err = WithExec(
		"INSERT INTO topics_vec(topic_rowid, overview_vector) VALUES (?, ?)",
		topicRowID, vec,
	)
	return err
}

type TopicVectorResult struct {
	TopicRowID int64
	Distance   float64
}

func QueryTopicVectors(query []float32, limit int) ([]TopicVectorResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT topic_rowid, distance
		FROM topics_vec
		WHERE overview_vector MATCH ?
		ORDER BY distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TopicVectorResult
	for rows.Next() {
		var r TopicVectorResult
		if err := rows.Scan(&r.TopicRowID, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func DeleteTopicVectorByRowID(rowID int64) error {
	_, err := WithExec("DELETE FROM topics_vec WHERE topic_rowid=?", rowID)
	return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/dao/ -run "TestUpsert|TestList|TestQuery" -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dao/topic.go internal/dao/topic_test.go internal/dao/db_init.go
git commit -m "feat: add topics table and topics_vec for hierarchical index"
```

---

### Task 3: LLM Client — llama-go generation

**Files:**
- Create: `internal/service/llm_client.go`
- Create: `internal/service/llm_client_test.go`

**Spec reference:** Section 6 (摘要生成), Section 4 设计文档 (Qwen3-4B-Instruct)

- [ ] **Step 1: Write failing test**

Create `internal/service/llm_client_test.go`:

```go
package service

import (
	"os"
	"testing"
)

func TestLLMClientGenerate(t *testing.T) {
	modelPath := os.Getenv("LMD_TEST_SUMMARIZE_MODEL")
	if modelPath == "" {
		t.Skip("LMD_TEST_SUMMARIZE_MODEL not set, skipping LLM test")
	}

	client, err := NewLLMClient(modelPath, -1, 4)
	if err != nil {
		t.Fatalf("NewLLMClient failed: %v", err)
	}
	defer client.Close()

	prompt := "用一句话总结：Go 语言的主要特点是简洁、并发和高效。"
	text, err := client.Generate(prompt, 128)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty output")
	}
	t.Logf("LLM output: %s", text)
}

func TestLLMClientNotExist(t *testing.T) {
	_, err := NewLLMClient("/nonexistent/model.gguf", -1, 4)
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestLLMClient -v`
Expected: FAIL - `undefined: NewLLMClient`

- [ ] **Step 3: Write llm_client.go implementation**

Create `internal/service/llm_client.go`:

```go
package service

import (
	"fmt"
	"os"
	"strings"
	"sync"

	llama "github.com/tcpipuk/llama-go"
)

type LLMClient struct {
	modelPath string
	gpuLayers int
	threads   int

	mu    sync.Mutex
	model *llama.Model
	lctx  *llama.Context
}

func NewLLMClient(modelPath string, gpuLayers, threads int) (*LLMClient, error) {
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("summarize model not found: %s", modelPath)
	}
	return &LLMClient{
		modelPath: modelPath,
		gpuLayers: gpuLayers,
		threads:   threads,
	}, nil
}

func (my *LLMClient) Generate(prompt string, maxTokens int) (string, error) {
	my.mu.Lock()
	defer my.mu.Unlock()

	if err := my.loadLocked(); err != nil {
		return "", err
	}

	// build message with Qwen3 chat template
	tokens, err := my.lctx.Model().Tokenize(
		fmt.Sprintf("<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n", prompt),
		true, false,
	)
	if err != nil {
		return "", fmt.Errorf("tokenize failed: %w", err)
	}

	if err := my.lctx.AddBatch(tokens, false); err != nil {
		return "", fmt.Errorf("add batch failed: %w", err)
	}

	var b strings.Builder
	for i := 0; i < maxTokens; i++ {
		token, err := my.lctx.Sample(my.lctx.Model().TokenEOS())
		if err != nil {
			break
		}
		if token == my.lctx.Model().TokenEOS() {
			break
		}
		b.WriteString(my.lctx.Model().TokenToPiece(token))
		if err := my.lctx.AddBatch([]int{token}, true); err != nil {
			break
		}
	}

	my.lctx.Clear()
	return b.String(), nil
}

func (my *LLMClient) loadLocked() error {
	if my.model != nil {
		return nil
	}

	model, err := llama.LoadModel(my.modelPath, llama.WithGPULayers(my.gpuLayers))
	if err != nil {
		return fmt.Errorf("load summarize model failed: %w", err)
	}

	lctx, err := model.NewContext(
		llama.WithThreads(my.threads),
	)
	if err != nil {
		model.Close()
		return fmt.Errorf("create context failed: %w", err)
	}

	my.model = model
	my.lctx = lctx
	return nil
}

func (my *LLMClient) Close() error {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.lctx != nil {
		my.lctx.Close()
		my.lctx = nil
	}
	if my.model != nil {
		my.model.Close()
		my.model = nil
	}
	return nil
}
```

- [ ] **Step 4: Set test env and run**

Run: `LMD_TEST_SUMMARIZE_MODEL=/nonexistent go test ./internal/service/ -run TestLLMClientNotExist -v`
Expected: PASS (correctly returns error)

Run: `LMD_TEST_SUMMARIZE_MODEL=~/.cache/lmd/models/Qwen3-4B-Instruct-2507-Q4_K_M.gguf go test ./internal/service/ -run TestLLMClientGenerate -v`
Expected: PASS or SKIP (if model not downloaded)

- [ ] **Step 5: Commit**

```bash
git add internal/service/llm_client.go internal/service/llm_client_test.go
git commit -m "feat: add LLM client for Qwen3-4B-Instruct text generation"
```

---

### Task 4: Topic Indexer — _topic.md generation pipeline

**Files:**
- Create: `internal/service/topic_indexer.go`
- Create: `internal/service/topic_indexer_test.go`

**Spec reference:** Section 6 (摘要生成), Section 3 (SPOT / hash detection)

- [ ] **Step 1: Write test for _topic.md parsing**

Create `internal/service/topic_indexer_test.go`:

```go
package service

import (
	"strings"
	"testing"
)

const sampleTopicMD = `# 数据库优化

> 本目录包含数据库性能优化相关资料，涵盖索引策略、查询优化、分库分表。

## 关键主题
- MySQL 索引优化
- 查询计划分析

## 文档
- mysql-index.md — MySQL索引类型与使用场景
- query-plan.md — EXPLAIN 输出解读

## 语义分组
- **索引策略** (2篇): mysql-index.md, btree-hash.md
- **查询优化** (1篇): query-plan.md
`

func TestParseTopicMD(t *testing.T) {
	topic, err := parseTopicMD(sampleTopicMD)
	if err != nil {
		t.Fatalf("parseTopicMD failed: %v", err)
	}
	if topic.Title != "数据库优化" {
		t.Errorf("title: got %q", topic.Title)
	}
	if !strings.Contains(topic.Overview, "数据库性能优化") {
		t.Errorf("overview mismatch: %s", topic.Overview)
	}
	if len(topic.Documents) != 2 {
		t.Errorf("documents: want 2, got %d", len(topic.Documents))
	}
	if topic.Documents[0].Path != "mysql-index.md" {
		t.Errorf("doc[0].path: got %q", topic.Documents[0].Path)
	}
	if topic.Documents[0].Desc != "MySQL索引类型与使用场景" {
		t.Errorf("doc[0].desc: got %q", topic.Documents[0].Desc)
	}
	if len(topic.SemanticGroups) != 2 {
		t.Errorf("semantic groups: want 2, got %d", len(topic.SemanticGroups))
	}
	if topic.SemanticGroups[0].Name != "索引策略" {
		t.Errorf("group[0].name: got %q", topic.SemanticGroups[0].Name)
	}
}

func TestBuildSummarizePrompt(t *testing.T) {
	docs := []docPreview{
		{Path: "a.md", Title: "Title A", Preview: "Content of document A"},
		{Path: "b.md", Title: "Title B", Preview: "Content of document B"},
	}
	prompt := buildSummarizePrompt("/notes/db", docs)
	if !strings.Contains(prompt, "/notes/db") {
		t.Error("prompt missing dir path")
	}
	if !strings.Contains(prompt, "Title A") {
		t.Error("prompt missing doc title")
	}
}

func TestTopicIndexerHashCheck(t *testing.T) {
	// existing hash matches → should re-summarize
	if !shouldSummarize("abc123", "abc123") {
		t.Error("should re-summarize when hash matches")
	}
	// hash changed (human edited) → skip
	if shouldSummarize("abc123", "def456") {
		t.Error("should skip when hash changed (human edited)")
	}
	// no existing hash → summarize (first time)
	if !shouldSummarize("", "new123") {
		t.Error("should summarize on first run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run "TestParse|TestBuild|TestTopicIndexer" -v`
Expected: FAIL

- [ ] **Step 3: Write topic_indexer.go implementation**

Create `internal/service/topic_indexer.go`:

```go
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/logo"
)

type docPreview struct {
	Path    string
	Title   string
	Preview string
}

type parsedTopic struct {
	Title          string
	Overview       string
	KeyTopics      []string
	Documents      []topicDoc
	SemanticGroups []topicGroup
}

type topicDoc struct {
	Path string
	Desc string
}

type topicGroup struct {
	Name    string
	Count   int
	DocRefs []string
}

type TopicIndexer struct {
	llm      *LLMClient
	provider embedding.EmbeddingProvider
	cooldown time.Duration
}

func NewTopicIndexer(llm *LLMClient, provider embedding.EmbeddingProvider, cooldown time.Duration) *TopicIndexer {
	return &TopicIndexer{
		llm:      llm,
		provider: provider,
		cooldown: cooldown,
	}
}

func (my *TopicIndexer) SummarizeDir(ctx context.Context, collection, dirPath, relPath string) error {
	// gather doc previews from the directory
	docs, err := my.gatherDocs(collection, dirPath, relPath)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		logo.Info("TopicIndexer: skip empty dir %s/%s", collection, relPath)
		return nil
	}

	// build prompt, call LLM
	prompt := buildSummarizePrompt(relPath, docs)
	markdown, genErr := my.llm.Generate(prompt, 2048)
	if genErr != nil {
		return fmt.Errorf("llm generate failed: %w", genErr)
	}

	// write _topic.md
	topicPath := filepath.Join(dirPath, "_topic.md")
	if err := os.WriteFile(topicPath, []byte(markdown), 0644); err != nil {
		return fmt.Errorf("write _topic.md failed: %w", err)
	}

	// parse and store in DB
	return my.storeTopic(ctx, collection, relPath, markdown)
}

func (my *TopicIndexer) gatherDocs(collection, dirPath, relPath string) ([]docPreview, error) {
	// read documents from DB for this collection + path prefix
	prefix := relPath
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	// query documents whose path starts with prefix and has no deeper subdir beyond
	docs, err := dao.GetDocumentsInDir(collection, prefix)
	if err != nil {
		return nil, err
	}

	var previews []docPreview
	for _, d := range docs {
		// extract relative path from full path
		docRelPath := strings.TrimPrefix(d.Path, prefix)
		if docRelPath == "" || strings.Contains(docRelPath, "/") {
			continue // skip empty or nested dir docs
		}

		body := d.Body
		runes := []rune(body)
		preview := body
		if len(runes) > 200 {
			preview = string(runes[:200])
		}

		previews = append(previews, docPreview{
			Path:    docRelPath,
			Title:   d.Title,
			Preview: preview,
		})
	}
	return previews, nil
}

func buildSummarizePrompt(dirPath string, docs []docPreview) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("你是一个知识库索引助手。请阅读以下目录中的文档标题和摘要，生成一个 _topic.md 索引文件。\n\n目录: %s\n文档数量: %d\n\n文档列表:\n", dirPath, len(docs)))
	for _, d := range docs {
		b.WriteString(fmt.Sprintf("--- %s: %s\n%s\n", d.Path, d.Title, d.Preview))
	}
	b.WriteString("\n请按以下格式生成 _topic.md（只输出 markdown，不要额外解释）：\n\n# <简短目录标题>\n> <2-3句概述>\n## 关键主题\n- <5-8个核心主题词>\n## 文档\n- `filename.md` — <一句话描述>\n## 语义分组\n- **<分组名>** (N篇): file1.md, file2.md, ...\n")
	return b.String()
}

func parseTopicMD(markdown string) (*parsedTopic, error) {
	t := &parsedTopic{}
	lines := strings.Split(markdown, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## "):
			t.Title = strings.TrimPrefix(line, "# ")
		case strings.HasPrefix(line, "> "):
			t.Overview = strings.TrimPrefix(line, "> ")
		case strings.HasPrefix(line, "- ") && isDocLine(line):
			doc := parseDocLine(line)
			t.Documents = append(t.Documents, doc)
		case strings.HasPrefix(line, `- **`) && strings.Contains(line, "篇"):
			group := parseGroupLine(line)
			t.SemanticGroups = append(t.SemanticGroups, group)
		case strings.HasPrefix(line, "- ") && i > 0:
			// in 关键主题 section
			prevLine := ""
			if i > 0 {
				prevLine = lines[i-1]
			}
			if strings.Contains(prevLine, "关键主题") || len(t.KeyTopics) > 0 {
				topic := strings.TrimPrefix(line, "- ")
				t.KeyTopics = append(t.KeyTopics, topic)
			}
		}
	}
	return t, nil
}

var docRefRe = regexp.MustCompile("`([^`]+)`")

func isDocLine(line string) bool {
	return docRefRe.MatchString(line) && !strings.Contains(line, "篇")
}

func parseDocLine(line string) topicDoc {
	matches := docRefRe.FindStringSubmatch(line)
	path := ""
	if len(matches) > 1 {
		path = matches[1]
	}
	// extract description after " — " or " - "
	desc := ""
	for _, sep := range []string{" — ", " - "} {
		idx := strings.Index(line, sep)
		if idx >= 0 {
			desc = strings.TrimSpace(line[idx+len(sep):])
			break
		}
	}
	return topicDoc{Path: path, Desc: desc}
}

var groupRe = regexp.MustCompile(`\*\*(.+?)\*\*\s*\((\d+)篇\)`)

func parseGroupLine(line string) topicGroup {
	matches := groupRe.FindStringSubmatch(line)
	name := ""
	count := 0
	if len(matches) > 1 {
		name = matches[1]
	}
	if len(matches) > 2 {
		fmt.Sscanf(matches[2], "%d", &count)
	}
	// extract file references after ":"
	idx := strings.Index(line, "):")
	if idx < 0 {
		idx = strings.Index(line, ":")
	}
	if idx >= 0 {
		rest := strings.TrimSpace(line[idx+1:])
		parts := strings.Split(rest, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				nameMatch := docRefRe.FindStringSubmatch(p)
				if len(nameMatch) > 1 {
					nameMatch[1] = docRefRe.FindString(p)
					// remove backticks
					p = strings.Trim(p, "`")
				}
			}
		}
	}
	return topicGroup{Name: name, Count: count}
}

func (my *TopicIndexer) storeTopic(ctx context.Context, collection, relPath, markdown string) error {
	parsed, err := parseTopicMD(markdown)
	if err != nil {
		return fmt.Errorf("parse _topic.md failed: %w", err)
	}

	hash := Sha256Hex(markdown)

	// collect doc paths
	var docPaths []string
	for _, d := range parsed.Documents {
		docPaths = append(docPaths, d.Path)
	}
	docPathsJSON := "[" + strings.Join(quoteStrings(docPaths), ",") + "]"

	if err := dao.UpsertTopic(collection, relPath, parsed.Overview, docPathsJSON, hash); err != nil {
		return fmt.Errorf("upsert topic failed: %w", err)
	}

	// embed overview and store vector
	rowID, err := dao.GetTopicRowID(collection, relPath)
	if err != nil {
		return fmt.Errorf("get topic rowid failed: %w", err)
	}

	// delete old vector first
	_ = dao.DeleteTopicVectorByRowID(rowID)

	if my.provider != nil {
		vec, err := my.provider.Embed(ctx, parsed.Overview)
		if err != nil {
			logo.Warn("TopicIndexer: embed topic overview failed: %s", err)
			return nil // non-fatal
		}
		if err := dao.UpsertTopicVector(rowID, vec); err != nil {
			logo.Warn("TopicIndexer: upsert topic vector failed: %s", err)
			return nil // non-fatal
		}
	}
	return nil
}

func Sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func ShouldSummarize(existingHash, newHash string) bool {
	if existingHash == "" {
		return true // first time
	}
	return existingHash == newHash // only if LLM-generated, not human-edited
}

func quoteStrings(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = `"` + s + `"`
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -run "TestParseTopicMD|TestBuildSummarizePrompt|TestTopicIndexerHash" -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/topic_indexer.go internal/service/topic_indexer_test.go
git commit -m "feat: add TopicIndexer for _topic.md generation and parsing"
```

---

### Task 5: Topic Router — embedding-based query routing

**Files:**
- Create: `internal/service/topic_router.go`
- Create: `internal/service/topic_router_test.go`

**Spec reference:** Section 7 (查询路由)

- [ ] **Step 1: Write failing test**

Create `internal/service/topic_router_test.go`:

```go
package service

import (
	"context"
	"testing"
)

func TestRouteQuery(t *testing.T) {
	router := &TopicRouter{}
	queryVec := make([]float32, 1024)

	// no topics → fallback
	collections, docIDs, err := router.RouteQuery(context.Background(), queryVec)
	if err != nil {
		t.Fatalf("RouteQuery failed: %v", err)
	}
	if len(collections) != 0 {
		t.Errorf("expected 0 collections for empty topics, got %d", len(collections))
	}
	if len(docIDs) != 0 {
		t.Errorf("expected 0 docIDs for empty topics, got %d", len(docIDs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestRouteQuery -v`
Expected: FAIL

- [ ] **Step 3: Write topic_router.go implementation**

Create `internal/service/topic_router.go`:

```go
package service

import (
	"encoding/json"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/logo"
)

const (
	topicTopK = 3 // 路由时取 Top-K 个匹配目录
)

type TopicRouter struct{}

func NewTopicRouter() *TopicRouter {
	return &TopicRouter{}
}

func (my *TopicRouter) Route(queryVec []float32) ([]string, map[int64]bool, error) {
	vecResults, err := dao.QueryTopicVectors(queryVec, topicTopK)
	if err != nil {
		return nil, nil, err
	}

	if len(vecResults) == 0 {
		// no topics → caller should fall back to full search
		return nil, nil, nil
	}

	// collect unique collections from matched topics
	collections := make(map[string]bool)
	docIDSet := make(map[int64]bool)

	for _, r := range vecResults {
		topic, err := dao.GetTopicByRowID(r.TopicRowID)
		if err != nil {
			logo.Warn("TopicRouter: get topic by rowID %d failed: %s", r.TopicRowID, err)
			continue
		}
		collections[topic.Collection] = true

		// parse doc paths and resolve to document IDs
		var docPaths []string
		if err := json.Unmarshal([]byte(topic.DocPaths), &docPaths); err != nil {
			logo.Warn("TopicRouter: parse doc_paths failed for %s/%s: %s", topic.Collection, topic.RelPath, err)
			continue
		}
		for _, p := range docPaths {
			fullPath := topic.RelPath
			if fullPath != "" && !jsonHasTrailingSlash(fullPath) {
				fullPath += "/"
			}
			fullPath += p
			doc, err := dao.GetDocumentByPath(topic.Collection, fullPath)
			if err != nil {
				continue
			}
			docIDSet[doc.Id] = true
		}
	}

	var colList []string
	for c := range collections {
		colList = append(colList, c)
	}
	logo.Info("TopicRouter: matched %d topics → %d collections, %d docs",
		len(vecResults), len(colList), len(docIDSet))
	return colList, docIDSet, nil
}

// SearchInDocs performs hybrid search within a set of document IDs.
func (my *TopicRouter) SearchInDocs(searcher *Searcher, provider embedding.EmbeddingProvider, query string, docIDs map[int64]bool, limit int, strategy string) ([]formatter.SearchHit, error) {
	// overfetch to compensate for doc filtering
	fetchLimit := limit * 5
	if fetchLimit > 500 {
		fetchLimit = 500
	}

	// BM25 search (no collection filter — we filter by docIDs afterward)
	lexHits, lexErr := searcher.SearchLex(query, "", fetchLimit, 0, strategy)
	if lexErr != nil {
		logo.Warn("TopicRouter: SearchLex failed: %s", lexErr)
	}

	// Vector search
	vecHits, vecErr := searcher.SearchVector(context.Background(), provider, query, "", fetchLimit, 0)
	if vecErr != nil {
		logo.Warn("TopicRouter: SearchVector failed: %s", vecErr)
	}

	// filter by docIDs
	lexHits = filterHitsByDocIDs(lexHits, docIDs)
	vecHits = filterHitsByDocIDs(vecHits, docIDs)

	// RRF fusion
	results := FuseResults(lexHits, vecHits)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func jsonHasTrailingSlash(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '/'
}

func filterHitsByDocIDs(hits []formatter.SearchHit, docIDs map[int64]bool) []formatter.SearchHit {
	if len(docIDs) == 0 {
		return hits
	}
	// extract chunk IDs, look up their doc_ids, filter
	if len(hits) == 0 {
		return hits
	}
	chunkIDs := make([]int64, len(hits))
	for i, h := range hits {
		chunkIDs[i] = h.ChunkId
	}
	chunks, err := dao.GetChunksByIds(chunkIDs)
	if err != nil {
		return hits // fallback: return unfiltered
	}
	chunkDocMap := make(map[int64]int64, len(chunks))
	for _, c := range chunks {
		chunkDocMap[c.Id] = c.DocId
	}
	var filtered []formatter.SearchHit
	for _, h := range hits {
		docID, ok := chunkDocMap[h.ChunkId]
		if ok && docIDs[docID] {
			filtered = append(filtered, h)
		}
	}
	return filtered
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/service/ -run TestRouteQuery -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/topic_router.go internal/service/topic_router_test.go
git commit -m "feat: add TopicRouter for embedding-based query routing"
```

---

### Task 6: Daemon Integration — background ticker + /smart-query + MCP

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/daemon_routes.go`
- Modify: `internal/daemon/daemon_mcp.go`
- Modify: `internal/mcp/server.go`

**Spec reference:** Section 6 (摘要生成), Section 7 (查询路由)

- [ ] **Step 1: Add fields to Daemon struct**

In `internal/daemon/daemon.go`, add to the `Daemon` struct:

```go
topicIndexer *service.TopicIndexer
topicRouter  *service.TopicRouter
llmClient    *service.LLMClient
```

- [ ] **Step 2: Initialize in NewDaemon / Start**

In `Start()` method, after initializing `embedder` (around line 100), add:

```go
if cfg.Topic.SummarizeModel != "" {
	llm, err := service.NewLLMClient(
		cfg.Topic.SummarizeModel,
		cfg.Topic.SummarizeGPULayers,
		cfg.Topic.SummarizeThreads,
	)
	if err != nil {
		logo.Warn("daemon: LLM client init failed: %s (smart query disabled)", err)
	} else {
		my.llmClient = llm
		my.topicIndexer = service.NewTopicIndexer(llm, my.provider, time.Duration(cfg.Topic.CooldownSeconds)*time.Second)
	}
}
my.topicRouter = service.NewTopicRouter()
```

- [ ] **Step 3: Add topicSyncTicker to goLoop**

In `goLoop()`, add a topic sync ticker after `embedTicker`:

```go
var topicCooldownSeconds = cfg.Topic.CooldownSeconds
if topicCooldownSeconds <= 0 {
	topicCooldownSeconds = 300 // 5 min default
}
var topicSyncTicker = later.NewTicker(time.Duration(topicCooldownSeconds) * time.Second)
for {
	select {
	case <-my.stopCh:
		return
	case <-syncIndexTicker.C:
		my.syncIndex()
	case <-topicSyncTicker.C:
		my.syncTopics()
	case <-embedTicker.C:
		my.embedChunks()
		my.provider.ReleaseIfIdle(modelIdleTimeout)
	}
}
```

- [ ] **Step 4: Add syncTopics method**

In `internal/daemon/daemon.go`, add:

```go
func (my *Daemon) syncTopics() {
	if my.topicIndexer == nil {
		return
	}
	my.rebuildMu.RLock()
	defer my.rebuildMu.RUnlock()

	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("syncTopics: list collections failed: %s", err)
		return
	}
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue // skip system collections
		}
		my.syncCollectionTopics(col)
	}
}

func (my *Daemon) syncCollectionTopics(col dao.CollectionRecord) {
	dirs, err := my.walkDirs(col.Path)
	if err != nil {
		logo.Error("syncTopics: walk %s failed: %s", col.Path, err)
		return
	}
	for _, dir := range dirs {
		relPath, _ := filepath.Rel(col.Path, dir)
		if relPath == "." {
			relPath = ""
		}
		my.syncDirTopic(col.Name, dir, relPath)
	}
}

func (my *Daemon) walkDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs, err
}

func (my *Daemon) syncDirTopic(collection, dirPath, relPath string) {
	topicPath := filepath.Join(dirPath, "_topic.md")

	// check if _topic.md exists
	info, statErr := os.Stat(topicPath)
	if statErr == nil {
		// check cooldown: don't re-summarize too frequently
		if time.Since(info.ModTime()) < time.Duration(my.cfg.Topic.CooldownSeconds)*time.Second {
			return
		}
	}

	// compare hash: skip if human-edited
	currentHash := ""
	if data, err := os.ReadFile(topicPath); err == nil {
		currentHash = service.Sha256Hex(string(data))
	}
	existing, _ := dao.GetTopic(collection, relPath)
	if existing != nil && !service.ShouldSummarize(existing.Hash, currentHash) {
		logo.Info("syncTopics: skip %s/%s (hash changed, human edited)", collection, relPath)
		return
	}

	if err := my.topicIndexer.SummarizeDir(context.Background(), collection, dirPath, relPath); err != nil {
		logo.Error("syncTopics: summarize %s/%s failed: %s", collection, relPath, err)
	}
}
```

- [ ] **Step 5: Add /smart-query HTTP endpoint**

In `internal/daemon/daemon_routes.go`, add after `handleHyde`:

```go
func (my *Daemon) handleSmartQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		MinScore float64 `json:"min_score"`
		Strategy string `json:"strategy"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	// fallback: if no topic router or no topics, use regular /query
	if my.topicRouter == nil || my.provider == nil {
		lexHits, _ := my.searcher.SearchLex(req.Query, "", safeOverfetch(req.Limit), 0, req.Strategy)
		vecHits, _ := my.searcher.SearchVector(r.Context(), my.provider, req.Query, "", safeOverfetch(req.Limit), 0)
		results := service.FuseResults(lexHits, vecHits)
		results = filterAndLimit(results, req.MinScore, req.Limit)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	queryVec, err := my.provider.EmbedQuery(r.Context(), req.Query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	_, docIDs, err := my.topicRouter.Route(queryVec)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if len(docIDs) == 0 {
		// no matching topics, fallback
		lexHits, _ := my.searcher.SearchLex(req.Query, "", safeOverfetch(req.Limit), 0, req.Strategy)
		vecHits, _ := my.searcher.SearchVector(r.Context(), my.provider, req.Query, "", safeOverfetch(req.Limit), 0)
		results := service.FuseResults(lexHits, vecHits)
		results = filterAndLimit(results, req.MinScore, req.Limit)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	results, err := my.topicRouter.SearchInDocs(my.searcher, my.provider, req.Query, docIDs, req.Limit, req.Strategy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.MinScore > 0 {
		results = filterAndLimit(results, req.MinScore, req.Limit)
	}

	logo.Info("handleSmartQuery: query=%q docs=%d hits=%d", req.Query, len(docIDs), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": true})
}
```

Register the route in `registerRoutes()` (in `server.go`):

```go
mux.HandleFunc("/smart-query", my.handleSmartQuery)
```

- [ ] **Step 6: Add smart_query MCP tool**

In `internal/mcp/server.go`, append to `toolDefs`:

```go
{Name: "smart_query", Description: "Hierarchical smart search using topic index routing"},
```

In `internal/daemon/daemon_mcp.go`, add to `handleToolCall` switch:

```go
case "smart_query":
	return my.handleToolSmartQuery(params)
```

Add handler method:

```go
func (my *Daemon) handleToolSmartQuery(params json.RawMessage) (interface{}, error) {
	var req struct {
		Query    string  `json:"query"`
		Limit    int     `json:"limit"`
		MinScore float64 `json:"min_score"`
		Strategy string  `json:"strategy"`
	}
	if err := convert.FromJsonE(params, &req); err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	// fallback
	if my.topicRouter == nil || my.provider == nil {
		lexHits, _ := my.searcher.SearchLex(req.Query, "", safeOverfetch(req.Limit), 0, req.Strategy)
		vecHits, _ := my.searcher.SearchVector(context.Background(), my.provider, req.Query, "", safeOverfetch(req.Limit), 0)
		results := service.FuseResults(lexHits, vecHits)
		results = filterAndLimit(results, req.MinScore, req.Limit)
		return map[string]interface{}{"hits": results, "routed": false}, nil
	}

	queryVec, err := my.provider.EmbedQuery(context.Background(), req.Query)
	if err != nil {
		return nil, err
	}

	_, docIDs, err := my.topicRouter.Route(queryVec)
	if err != nil {
		return nil, err
	}
	if len(docIDs) == 0 {
		lexHits, _ := my.searcher.SearchLex(req.Query, "", safeOverfetch(req.Limit), 0, req.Strategy)
		vecHits, _ := my.searcher.SearchVector(context.Background(), my.provider, req.Query, "", safeOverfetch(req.Limit), 0)
		results := service.FuseResults(lexHits, vecHits)
		results = filterAndLimit(results, req.MinScore, req.Limit)
		return map[string]interface{}{"hits": results, "routed": false}, nil
	}

	results, err := my.topicRouter.SearchInDocs(my.searcher, my.provider, req.Query, docIDs, req.Limit, req.Strategy)
	if err != nil {
		return nil, err
	}
	if req.MinScore > 0 {
		results = filterAndLimit(results, req.MinScore, req.Limit)
	}
	return map[string]interface{}{"hits": results, "routed": true}, nil
}
```

- [ ] **Step 7: Clean up LLM client on shutdown**

In `Stop()` method, add before closing the store:

```go
if my.llmClient != nil {
	my.llmClient.Close()
}
```

- [ ] **Step 8: Verify compilation**

Run: `go build ./...`
Expected: compiles successfully

- [ ] **Step 9: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/daemon_routes.go internal/daemon/daemon_mcp.go internal/mcp/server.go internal/daemon/server.go internal/service/topic_indexer.go
git commit -m "feat: integrate hierarchical index into daemon with /smart-query and MCP tool"
```

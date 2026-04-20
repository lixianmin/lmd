# 内嵌 llama.cpp Embedding + HyDE 生成 实现计划

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 tcpipuk/llama-go CGo bindings 替换 Ollama HTTP API，daemon 进程内直接加载 GGUF 模型做 embedding 和 HyDE 生成。

**Architecture:** Git submodule 引入 llama-go，config 重构为 LlamaConfig 顶层，新增 LlamaProvider 和 ModelLifecycle 按需管理模型加载/释放，HyDE 改用 llama-go Generate。

**Tech Stack:** tcpipuk/llama-go (CGo), Qwen3-Embedding-0.6B-Q8_0.gguf, Qwen3-0.6B-Q8_0.gguf, Metal GPU (macOS M4)

**Spec:** `docs/superpowers/specs/2026-04-20-llamacpp-embedded-design.md`

**Build commands:**
- Build: `make build`
- Test: `go test -tags fts5 -mod=mod ./...`
- 注意：本计划中的 llama-go 相关测试需要 GGUF 模型文件，单元测试使用 mock 接口

---

## Chunk 1: Submodule 集成 + Config 重构

### Task 1: 添加 llama-go Git submodule

**Files:**
- Modify: `.gitmodules`
- Modify: `go.mod`
- Create: `llama-go/` (submodule directory)

- [ ] **Step 1: 添加 submodule**

```bash
git submodule add https://github.com/tcpipuk/llama-go.git llama-go
```

- [ ] **Step 2: 编译 libbinding.a (Metal build)**

```bash
cd llama-go && BUILD_TYPE=metal make libbinding.a && cd ..
```

- [ ] **Step 3: 添加 go.mod replace directive**

在 `go.mod` 末尾添加：

```
replace github.com/tcpipuk/llama-go => ./llama-go
```

- [ ] **Step 4: go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 5: 验证编译**

```bash
LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go go build -tags fts5 -mod=mod ./...
```

- [ ] **Step 6: Commit**

```bash
git add .gitmodules llama-go go.mod go.sum
git commit -m "chore: add tcpipuk/llama-go submodule for embedded inference"
```

### Task 2: Config 重构 — 新增 LlamaConfig，移除 OllamaConfig

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: 写失败测试 — TestDefaultConfig_LlamaFields**

修改 `internal/config/config_test.go`，更新 `TestDefaultConfig`：

```go
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Daemon.Port != 12345 {
		t.Fatalf("expected port 12345, got %d", cfg.Daemon.Port)
	}
	if cfg.Llama.EmbedModel == "" {
		t.Fatal("expected non-empty llama.embed_model")
	}
	if cfg.Llama.HydeModel == "" {
		t.Fatal("expected non-empty llama.hyde_model")
	}
	if cfg.Llama.GPULayers != -1 {
		t.Fatalf("expected gpu_layers -1, got %d", cfg.Llama.GPULayers)
	}
	if cfg.Llama.Parallel != 8 {
		t.Fatalf("expected parallel 8, got %d", cfg.Llama.Parallel)
	}
	if cfg.Llama.Threads != 4 {
		t.Fatalf("expected threads 4, got %d", cfg.Llama.Threads)
	}
	if cfg.Embedding.BatchSize != 8 {
		t.Fatalf("expected batch_size 8, got %d", cfg.Embedding.BatchSize)
	}
	if cfg.Vector.Dimensions != 1024 {
		t.Fatalf("expected dimensions 1024, got %d", cfg.Vector.Dimensions)
	}
	if cfg.Embedding.Truncation != 800 {
		t.Fatalf("expected truncation 800, got %d", cfg.Embedding.Truncation)
	}
	if cfg.Daemon.IdleTimeout != "30m" {
		t.Fatalf("expected idle_timeout 30m, got %s", cfg.Daemon.IdleTimeout)
	}
	if cfg.Daemon.IndexPollInterval != "30s" {
		t.Fatalf("expected index_poll_interval 30s, got %s", cfg.Daemon.IndexPollInterval)
	}
}
```

- [ ] **Step 2: 写失败测试 — TestLoadPartialConfig_LlamaFields**

```go
func TestLoadPartialConfig_LlamaFields(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	defer func() { configDir = orig }()

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("daemon:\n  port: 19999\n"), 0644)

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", loaded.Daemon.Port)
	}
	if loaded.Llama.GPULayers != -1 {
		t.Fatalf("expected default gpu_layers -1, got %d", loaded.Llama.GPULayers)
	}
	if loaded.Embedding.BatchSize != 8 {
		t.Fatalf("expected default batch_size 8, got %d", loaded.Embedding.BatchSize)
	}
	if !loaded.HyDE.Enabled {
		t.Fatal("expected default HyDE enabled=true")
	}
}
```

- [ ] **Step 3: 写失败测试 — TestLoadMissingReturnsDefault_12345**

```go
func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	orig := configDir
	configDir = dir
	defer func() { configDir = orig }()

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 12345 {
		t.Fatalf("expected default port 12345, got %d", loaded.Daemon.Port)
	}
}
```

- [ ] **Step 4: 运行测试确认失败**

```bash
go test -tags fts5 -mod=mod ./internal/config/ -run "TestDefaultConfig|TestLoadPartial|TestLoadMissing" -v
```

- [ ] **Step 5: 重构 config.go — 新结构体**

修改 `internal/config/config.go`：

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

删除 `OllamaConfig` 结构体。删除 `EmbeddingConfig` 中的 `Provider` 和 `Ollama` 字段。删除 `HyDEConfig` 中的 `Model` 字段。

- [ ] **Step 6: 重构 DefaultConfig()**

```go
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Daemon: DaemonConfig{
			Port:              12345,
			IdleTimeout:       "30m",
			IndexPollInterval: "30s",
		},
		Llama: LlamaConfig{
			EmbedModel:       filepath.Join(home, ".cache", "lmd", "models", "Qwen3-Embedding-0.6B-Q8_0.gguf"),
			HydeModel:        filepath.Join(home, ".cache", "lmd", "models", "Qwen3-0.6B-Q8_0.gguf"),
			GPULayers:        -1,
			ModelIdleTimeout: "10m",
			Parallel:         8,
			Threads:          4,
		},
		Embedding: EmbeddingConfig{
			BatchSize:  8,
			Truncation: 800,
		},
		HyDE: HyDEConfig{
			Enabled:   true,
			MaxTokens: 200,
		},
		Vector: VectorConfig{
			Dimensions:     1024,
			DistanceMetric: "cosine",
		},
		Database: DatabaseConfig{
			Path: filepath.Join(home, ".cache", "lmd", "index.sqlite"),
		},
	}
}
```

- [ ] **Step 7: 更新所有引用 OllamaConfig 的代码**

需要搜索并更新以下文件中对 `cfg.Embedding.Ollama` 的引用：
- `internal/daemon/daemon.go` — `my.cfg.Embedding.Ollama.URL` 和 `my.cfg.Embedding.Ollama.Model`
- `internal/daemon/routes.go` — handleQuery 中的 `my.cfg.Embedding.Ollama.URL` 和 `my.cfg.HyDE.Model`
- `internal/daemon/client.go` — 检查是否有 Ollama 引用

暂时将 daemon.go 中的 provider 创建注释掉（等 Chunk 2 实现 LlamaProvider 后替换）。

- [ ] **Step 8: 运行测试确认通过**

```bash
go test -tags fts5 -mod=mod ./internal/config/ -v
```

- [ ] **Step 9: Commit**

```bash
git add internal/config/
git commit -m "refactor: config restructure — LlamaConfig top-level, remove OllamaConfig"
```

---

## Chunk 2: LlamaProvider 实现

### Task 3: LlamaProvider — 实现 EmbeddingProvider 接口

**Files:**
- Create: `internal/embedding/llama.go`
- Create: `internal/embedding/llama_test.go`

- [ ] **Step 1: 写失败测试 — TestLlamaProvider_Interface**

`internal/embedding/llama_test.go`：

```go
package embedding

import (
	"context"
	"testing"
)

func TestLlamaProvider_Interface(t *testing.T) {
	var _ EmbeddingProvider = (*LlamaProvider)(nil)
}
```

- [ ] **Step 2: 写 LlamaProvider 结构体（不含 llama-go，先用 mock）**

由于 LlamaProvider 需要实际 GGUF 模型文件才能测试，我们先定义好接口实现的结构，测试时用 interface mock。

`internal/embedding/llama.go`：

```go
package embedding

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	llama "github.com/tcpipuk/llama-go"
	"github.com/lixianmin/logo"
)

type LlamaProvider struct {
	modelPath string
	gpuLayers int
	threads   int
	parallel  int
	dim       int

	mu         sync.Mutex
	model      *llama.Model
	ctx        *llama.Context
	lastActive time.Time
}

func NewLlamaProvider(modelPath string, gpuLayers, threads, parallel int) *LlamaProvider {
	return &LlamaProvider{
		modelPath: modelPath,
		gpuLayers: gpuLayers,
		threads:   threads,
		parallel:  parallel,
		dim:       1024,
	}
}

func (my *LlamaProvider) ensureLoaded() error {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.model != nil {
		my.lastActive = time.Now()
		return nil
	}

	if _, err := os.Stat(my.modelPath); os.IsNotExist(err) {
		return fmt.Errorf("model file not found: %s", my.modelPath)
	}

	model, err := llama.LoadModel(my.modelPath, llama.WithGPULayers(my.gpuLayers))
	if err != nil {
		return fmt.Errorf("load model failed: %w", err)
	}

	ctx, err := model.NewContext(
		llama.WithEmbeddings(),
		llama.WithThreads(my.threads),
		llama.WithParallel(my.parallel),
	)
	if err != nil {
		model.Close()
		return fmt.Errorf("create embedding context failed: %w", err)
	}

	my.model = model
	my.ctx = ctx
	my.lastActive = time.Now()
	logo.Info("LlamaProvider: model loaded from %s", my.modelPath)
	return nil
}

func (my *LlamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := my.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vecs[0], nil
}

func (my *LlamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := my.ensureLoaded(); err != nil {
		return nil, err
	}
	my.mu.Lock()
	defer my.mu.Unlock()

	vecs, err := my.ctx.GetEmbeddingsBatch(texts)
	if err != nil {
		return nil, fmt.Errorf("embedding batch failed: %w", err)
	}
	return vecs, nil
}

func (my *LlamaProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, query)
}

func (my *LlamaProvider) Dimension() int    { return my.dim }
func (my *LlamaProvider) ModelName() string { return my.modelPath }

func (my *LlamaProvider) Close() error {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.ctx != nil {
		my.ctx.Close()
		my.ctx = nil
	}
	if my.model != nil {
		my.model.Close()
		my.model = nil
	}
	logo.Info("LlamaProvider: model released")
	return nil
}

func (my *LlamaProvider) ReleaseIfIdle(timeout time.Duration) bool {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.model == nil {
		return false
	}
	if time.Since(my.lastActive) > timeout {
		my.ctx.Close()
		my.ctx = nil
		my.model.Close()
		my.model = nil
		logo.Info("LlamaProvider: model released after idle %s", timeout)
		return true
	}
	return false
}
```

- [ ] **Step 3: 写测试 — TestLlamaProvider_ModelNotFound**

```go
func TestLlamaProvider_ModelNotFound(t *testing.T) {
	p := NewLlamaProvider("/nonexistent/model.gguf", -1, 4, 8)
	_, err := p.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for missing model file")
	}
}
```

- [ ] **Step 4: 写测试 — TestLlamaProvider_Close_Idempotent**

```go
func TestLlamaProvider_Close_Idempotent(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	if err := p.Close(); err != nil {
		t.Fatalf("Close on unloaded provider should not error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Second Close should not error: %v", err)
	}
}
```

- [ ] **Step 5: 写测试 — TestLlamaProvider_Dimension**

```go
func TestLlamaProvider_Dimension(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	if p.Dimension() != 1024 {
		t.Fatalf("expected 1024, got %d", p.Dimension())
	}
}
```

- [ ] **Step 6: 写测试 — TestLlamaProvider_ReleaseIfIdle**

```go
func TestLlamaProvider_ReleaseIfIdle_NotLoaded(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	released := p.ReleaseIfIdle(time.Minute)
	if released {
		t.Fatal("should not release when not loaded")
	}
}

func TestLlamaProvider_ReleaseIfIdle_NotYetIdle(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	p.lastActive = time.Now()
	released := p.ReleaseIfIdle(10 * time.Minute)
	if released {
		t.Fatal("should not release when not idle")
	}
}
```

- [ ] **Step 7: 写测试 — TestLlamaProvider_EmbedBatch_EmptyInput**

```go
func TestLlamaProvider_EmbedBatch_EmptyInput(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	p.model = (*llama.Model)(nil)
	_, err := p.EmbedBatch(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error with nil model and empty input")
	}
}
```

注意：此测试会因为 model 为 nil 而失败（ensureLoaded 检查文件不存在）。这正是预期行为 — 空输入 + 未加载模型应该报错。

- [ ] **Step 8: 运行测试**

```bash
LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go go test -tags fts5 -mod=mod ./internal/embedding/ -run TestLlamaProvider -v
```

- [ ] **Step 9: Commit**

```bash
git add internal/embedding/llama.go internal/embedding/llama_test.go
git commit -m "feat: LlamaProvider — embedded llama.cpp embedding via tcpipuk/llama-go"
```

---

## Chunk 3: ModelLifecycle + 模型下载

### Task 4: ModelLifecycle — 按需加载 + 空闲释放

**Files:**
- Create: `internal/service/model_lifecycle.go`
- Create: `internal/service/model_lifecycle_test.go`

- [ ] **Step 1: 写失败测试 — TestModelLifecycle_StartStop**

`internal/service/model_lifecycle_test.go`：

```go
package service

import (
	"testing"
	"time"
)

type mockReleasable struct {
	released bool
}

func (m *mockReleasable) ReleaseIfIdle(timeout time.Duration) bool {
	m.released = true
	return true
}
func (m *mockReleasable) Close() error { return nil }

func TestModelLifecycle_ReleaseOnTimeout(t *testing.T) {
	mock := &mockReleasable{}
	lc := NewModelLifecycle(mock, 100*time.Millisecond)
	go lc.Run()
	defer lc.Stop()

	time.Sleep(200 * time.Millisecond)
	if !mock.released {
		t.Fatal("expected model to be released after idle timeout")
	}
}

func TestModelLifecycle_NotReleasedWhenActive(t *testing.T) {
	mock := &mockReleasable{}
	lc := NewModelLifecycle(mock, 500*time.Millisecond)
	go lc.Run()
	defer lc.Stop()

	lc.Touch()
	time.Sleep(100 * time.Millisecond)
	if mock.released {
		t.Fatal("model should not be released while active")
	}
}

func TestModelLifecycle_StopIsIdempotent(t *testing.T) {
	mock := &mockReleasable{}
	lc := NewModelLifecycle(mock, time.Minute)
	lc.Stop()
	lc.Stop()
}
```

- [ ] **Step 2: 实现 ModelLifecycle**

`internal/service/model_lifecycle.go`：

```go
package service

import (
	"sync"
	"time"

	"github.com/lixianmin/logo"
)

type idleReleaser interface {
	ReleaseIfIdle(timeout time.Duration) bool
	Close() error
}

type ModelLifecycle struct {
	releaser idleReleaser
	timeout  time.Duration
	done     chan struct{}
	mu       sync.Mutex
	lastActive time.Time
}

func NewModelLifecycle(releaser idleReleaser, timeout time.Duration) *ModelLifecycle {
	return &ModelLifecycle{
		releaser: releaser,
		timeout:  timeout,
		done:     make(chan struct{}),
	}
}

func (my *ModelLifecycle) Touch() {
	my.mu.Lock()
	defer my.mu.Unlock()
	my.lastActive = time.Now()
}

func (my *ModelLifecycle) Run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-my.done:
			return
		case <-ticker.C:
			my.releaser.ReleaseIfIdle(my.timeout)
		}
	}
}

func (my *ModelLifecycle) Stop() {
	select {
	case <-my.done:
	default:
		close(my.done)
		my.releaser.Close()
		logo.Info("ModelLifecycle: stopped")
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
go test -tags fts5 -mod=mod ./internal/service/ -run TestModelLifecycle -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/service/model_lifecycle.go internal/service/model_lifecycle_test.go
git commit -m "feat: ModelLifecycle — idle timeout model release"
```

### Task 5: 模型自动下载

**Files:**
- Create: `internal/service/downloader.go`
- Create: `internal/service/downloader_test.go`

- [ ] **Step 1: 写失败测试 — TestDownloadModel_FileExists**

```go
package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadModel_FileExists(t *testing.T) {
	dir := t.TempDir()
	fakeModel := filepath.Join(dir, "model.gguf")
	os.WriteFile(fakeModel, []byte("fake"), 0644)

	err := DownloadModel(fakeModel, "https://example.com/model.gguf")
	if err != nil {
		t.Fatalf("should not download when file exists: %v", err)
	}
}
```

- [ ] **Step 2: 写失败测试 — TestDownloadModel_DownloadSuccess**

```go
func TestDownloadModel_DownloadSuccess(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("gguf-model-data"))
	}))
	defer server.Close()

	err := DownloadModel(target, server.URL+"/model.gguf")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != "gguf-model-data" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}
```

- [ ] **Step 3: 写失败测试 — TestDownloadModel_MirrorFallback**

```go
func TestDownloadModel_MirrorFallback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	callCount := 0
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badServer.Close()

	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("from-mirror"))
	}))
	defer goodServer.Close()

	err := DownloadModel(target, badServer.URL+"/fail", goodServer.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(target)
	if string(data) != "from-mirror" {
		t.Fatalf("expected mirror content, got %s", string(data))
	}
	if callCount != 1 {
		t.Fatalf("expected primary to be tried once, got %d calls", callCount)
	}
}
```

- [ ] **Step 4: 写失败测试 — TestDownloadModel_AllFail**

```go
func TestDownloadModel_AllFail(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "model.gguf")

	err := DownloadModel(target, "http://127.0.0.1:1/fail1", "http://127.0.0.1:1/fail2")
	if err == nil {
		t.Fatal("expected error when all downloads fail")
	}
}
```

- [ ] **Step 5: 写失败测试 — TestDownloadModel_CreatesParentDir**

```go
func TestDownloadModel_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "subdir", "model.gguf")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer server.Close()

	err := DownloadModel(target, server.URL+"/model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		t.Fatal("file should exist after download")
	}
}
```

- [ ] **Step 6: 实现 DownloadModel**

`internal/service/downloader.go`：

```go
package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lixianmin/logo"
)

func DownloadModel(targetPath string, urls ...string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	for _, url := range urls {
		logo.Info("DownloadModel: downloading %s -> %s", url, targetPath)
		if err := downloadFile(targetPath, url); err != nil {
			logo.Warn("DownloadModel: %s failed: %s", url, err)
			continue
		}
		logo.Info("DownloadModel: success %s", targetPath)
		return nil
	}

	return fmt.Errorf("all download attempts failed for %s", targetPath)
}

func downloadFile(targetPath, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmpPath := targetPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, targetPath)
}
```

- [ ] **Step 7: 运行测试**

```bash
go test -tags fts5 -mod=mod ./internal/service/ -run TestDownloadModel -v
```

- [ ] **Step 8: Commit**

```bash
git add internal/service/downloader.go internal/service/downloader_test.go
git commit -m "feat: DownloadModel — auto-download GGUF with mirror fallback"
```

---

## Chunk 4: HyDE 内嵌 + Daemon 集成

### Task 6: HyDE — 用 llama-go Generate 替换 Ollama chat

**Files:**
- Modify: `internal/service/hyde.go`
- Modify: `internal/service/hyde_test.go`

- [ ] **Step 1: 写失败测试 — TestGenerateHypotheticalDocument_LlamaInterface**

在 `internal/service/hyde_test.go` 中新增：

```go
func TestHyDEGenerator_Interface(t *testing.T) {
	var _ HyDEModel = (*MockHyDEModel)(nil)
}

func TestNewHyDEGenerator(t *testing.T) {
	mock := &MockHyDEModel{response: "test document"}
	gen := NewHyDEGenerator(mock)
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
}

func TestHyDEGenerator_Generate(t *testing.T) {
	mock := &MockHyDEModel{response: "dark mode reduces eye strain"}
	gen := NewHyDEGenerator(mock)
	doc, err := gen.Generate(context.Background(), "dark mode preferences")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "dark mode reduces eye strain" {
		t.Fatalf("unexpected doc: %s", doc)
	}
}

func TestHyDEGenerator_EmptyResponse(t *testing.T) {
	mock := &MockHyDEModel{response: ""}
	gen := NewHyDEGenerator(mock)
	doc, err := gen.Generate(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if doc != "" {
		t.Fatalf("expected empty, got %q", doc)
	}
}

func TestHyDEGenerator_GenerateError(t *testing.T) {
	mock := &MockHyDEModel{err: fmt.Errorf("model error")}
	gen := NewHyDEGenerator(mock)
	_, err := gen.Generate(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: 重构 hyde.go — 抽象 HyDEModel 接口**

保留原有 `GenerateHypotheticalDocument`（Ollama 版本），新增接口和 llama-go 实现：

```go
type HyDEModel interface {
	Generate(ctx context.Context, prompt string, maxTokens int) (string, error)
}

type HyDEGenerator struct {
	model HyDEModel
}

func NewHyDEGenerator(model HyDEModel) *HyDEGenerator {
	return &HyDEGenerator{model: model}
}

func (g *HyDEGenerator) Generate(ctx context.Context, query string) (string, error) {
	prompt := fmt.Sprintf(
		"Given the following search query, write a short passage that would answer this query. Keep it under 200 words.\n\nQuery: %s",
		query,
	)
	return g.model.Generate(ctx, prompt, 200)
}
```

- [ ] **Step 3: 创建 LlamaHyDEModel（封装 llama-go 文本生成）**

在 `internal/service/hyde.go` 中新增：

```go
type LlamaHyDEModel struct {
	modelPath string
	gpuLayers int
	threads   int

	mu         sync.Mutex
	model      *llama.Model
	lastActive time.Time
}

func NewLlamaHyDEModel(modelPath string, gpuLayers, threads int) *LlamaHyDEModel {
	return &LlamaHyDEModel{
		modelPath: modelPath,
		gpuLayers: gpuLayers,
		threads:   threads,
	}
}

func (m *LlamaHyDEModel) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if err := m.ensureLoaded(); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	lctx, err := m.model.NewContext(
		llama.WithContext(2048),
		llama.WithThreads(m.threads),
	)
	if err != nil {
		return "", fmt.Errorf("hyde create context failed: %w", err)
	}
	defer lctx.Close()

	text, err := lctx.Generate(prompt, llama.WithMaxTokens(maxTokens))
	if err != nil {
		return "", fmt.Errorf("hyde generate failed: %w", err)
	}

	m.lastActive = time.Now()
	return strings.TrimSpace(text), nil
}

func (m *LlamaHyDEModel) ensureLoaded() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.model != nil {
		return nil
	}
	if _, err := os.Stat(m.modelPath); os.IsNotExist(err) {
		return fmt.Errorf("hyde model not found: %s", m.modelPath)
	}

	model, err := llama.LoadModel(m.modelPath, llama.WithGPULayers(m.gpuLayers))
	if err != nil {
		return fmt.Errorf("hyde load model failed: %w", err)
	}
	m.model = model
	m.lastActive = time.Now()
	logo.Info("LlamaHyDEModel: loaded %s", m.modelPath)
	return nil
}

func (m *LlamaHyDEModel) ReleaseIfIdle(timeout time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model == nil {
		return false
	}
	if time.Since(m.lastActive) > timeout {
		m.model.Close()
		m.model = nil
		logo.Info("LlamaHyDEModel: released after idle %s", timeout)
		return true
	}
	return false
}

func (m *LlamaHyDEModel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model != nil {
		m.model.Close()
		m.model = nil
	}
	return nil
}
```

- [ ] **Step 4: 在 hyde_test.go 中添加 mock**

```go
type MockHyDEModel struct {
	response string
	err      error
}

func (m *MockHyDEModel) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	return m.response, m.err
}
```

- [ ] **Step 5: 运行测试**

```bash
go test -tags fts5 -mod=mod ./internal/service/ -run "TestHyDE|TestGenerate" -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/hyde.go internal/service/hyde_test.go
git commit -m "feat: HyDE refactored — HyDEModel interface + LlamaHyDEModel"
```

### Task 7: Daemon 集成 — 替换 provider + 启动 ModelLifecycle

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/daemon/routes.go`
- Modify: `internal/daemon/daemon_test.go`

- [ ] **Step 1: 写失败测试 — TestDaemon_NewWithLlamaConfig**

在 `internal/daemon/daemon_test.go` 中新增：

```go
func TestDaemon_NewWithLlamaConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	d := NewDaemon(cfg)
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
}
```

- [ ] **Step 2: 更新 Daemon 结构体**

修改 `internal/daemon/daemon.go`：

```go
type Daemon struct {
	cfg       *config.Config
	server    *http.Server
	done      chan struct{}
	lastActive time.Time

	tokenizer    tokenizer.Tokenizer
	indexer      *service.Indexer
	searcher     *service.Searcher
	embedder     *service.Embedder
	provider     embedding.EmbeddingProvider
	memSvc       *service.MemoryService
	hydeModel    *service.LlamaHyDEModel
	embedLifecycle *service.ModelLifecycle
	hydeLifecycle  *service.ModelLifecycle
}
```

- [ ] **Step 3: 更新 Start() 方法 — 创建 LlamaProvider + LlamaHyDEModel**

替换 daemon.go 中 `Start()` 里的 provider 创建代码：

```go
llamaProvider := embedding.NewLlamaProvider(
	my.cfg.Llama.EmbedModel,
	my.cfg.Llama.GPULayers,
	my.cfg.Llama.Threads,
	my.cfg.Llama.Parallel,
)
my.provider = llamaProvider
my.indexer = service.NewIndexer(tok)
my.searcher = service.NewSearcher(tok)
my.embedder = service.NewEmbedder(my.provider)
my.memSvc = service.NewMemoryService(tok)

my.hydeModel = service.NewLlamaHyDEModel(
	my.cfg.Llama.HydeModel,
	my.cfg.Llama.GPULayers,
	my.cfg.Llama.Threads,
)

modelIdle, _ := time.ParseDuration(my.cfg.Llama.ModelIdleTimeout)
if modelIdle == 0 {
	modelIdle = 10 * time.Minute
}
my.embedLifecycle = service.NewModelLifecycle(llamaProvider, modelIdle)
my.hydeLifecycle = service.NewModelLifecycle(my.hydeModel, modelIdle)

go my.embedLifecycle.Run()
go my.hydeLifecycle.Run()
```

- [ ] **Step 4: 更新 Stop() — 关闭 ModelLifecycle**

在 `Stop()` 方法中 `close(my.done)` 之前添加：

```go
if my.embedLifecycle != nil {
	my.embedLifecycle.Stop()
}
if my.hydeLifecycle != nil {
	my.hydeLifecycle.Stop()
}
```

- [ ] **Step 5: 更新 routes.go — handleQuery 使用 LlamaHyDEModel**

修改 `handleQuery` 中 HyDE 部分：

将 `service.GenerateHypotheticalDocument(ctx, url, model, query)` 替换为：

```go
hydeGen := service.NewHyDEGenerator(my.hydeModel)
hydeDoc, hydeErr := hydeGen.Generate(context.Background(), req.Query)
```

- [ ] **Step 6: 添加模型下载逻辑到 Start()**

在 `Start()` 方法中，provider 创建之前添加：

```go
modelDir := filepath.Dir(my.cfg.Llama.EmbedModel)
embedURLs := []string{
	"https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf",
	"https://hf-mirror.com/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf",
}
if err := service.DownloadModel(my.cfg.Llama.EmbedModel, embedURLs...); err != nil {
	logo.Warn("daemon: embed model download failed: %s (will retry on first use)", err)
}

if my.cfg.HyDE.Enabled {
	hydeURLs := []string{
		"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf",
		"https://hf-mirror.com/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q8_0.gguf",
	}
	if err := service.DownloadModel(my.cfg.Llama.HydeModel, hydeURLs...); err != nil {
		logo.Warn("daemon: hyde model download failed: %s", err)
	}
}
```

- [ ] **Step 7: 运行所有测试**

```bash
LIBRARY_PATH=$PWD/llama-go C_INCLUDE_PATH=$PWD/llama-go go test -tags fts5 -mod=mod ./... -v
```

- [ ] **Step 8: Commit**

```bash
git add internal/daemon/
git commit -m "feat: daemon integration — LlamaProvider + LlamaHyDEModel + auto-download"
```

---

## Chunk 5: Makefile + 集成测试 + 清理

### Task 8: 更新 Makefile — submodule 初始化 + 编译

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: 更新 Makefile**

```makefile
.PHONY: build install test vet clean tidy fmt lint e2e integration integration-basic integration-vector submodule

BINARY  = lmd
PKG     = github.com/lixianmin/lmd
CMD     = $(PKG)/cmd/lmd
TAGS    = fts5
GO      = go
LDFLAGS = -s -w
MOD     = -mod=mod
LLAMA_DIR = llama-go

submodule:
	@if [ ! -f $(LLAMA_DIR)/libbinding.a ]; then \
		git submodule update --init --recursive; \
		cd $(LLAMA_DIR) && BUILD_TYPE=metal make libbinding.a && cd ..; \
	fi

build: submodule
	-./$(BINARY) daemon stop 2>/dev/null || true
	LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) \
		$(GO) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) -o $(BINARY) $(CMD)

install: submodule
	-$(GO) env GOPATH/bin/lmd daemon stop 2>/dev/null || true
	LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) \
		$(GO) install -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) $(CMD)

test: submodule
	LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) \
		$(GO) test -tags "$(TAGS)" -count=1 $(MOD) ./...

test-verbose: submodule
	LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) \
		$(GO) test -tags "$(TAGS)" -count=1 -v $(MOD) ./...

vet: submodule
	LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) \
		$(GO) vet -tags "$(TAGS)" $(MOD) ./...

tidy:
	$(GO) mod tidy

fmt:
	gofmt -w .

lint: vet fmt

clean:
	rm -f $(BINARY)

e2e: build
	@rm -rf /tmp/lmd-e2e
	@mkdir -p /tmp/lmd-e2e/docs
	@echo '# Go并发编程\n\nGo语言通过goroutine和channel实现并发编程。\ngoroutine是轻量级线程，channel用于goroutine间通信。' > /tmp/lmd-e2e/docs/go.md
	@echo '# Python数据科学\n\nPython是数据科学领域最流行的语言。\npandas和numpy是核心数据处理库。' > /tmp/lmd-e2e/docs/python.md
	@./$(BINARY) collection add /tmp/lmd-e2e/docs --name docs
	@./$(BINARY) search "并发"
	@./$(BINARY) status
	@rm -rf /tmp/lmd-e2e

integration-basic: install
	bash tests/test_basic.sh

integration-vector: install
	bash tests/test_vector.sh

integration: integration-basic

all: lint test integration-basic
```

- [ ] **Step 2: 运行 `make build` 验证**

```bash
make build
```

- [ ] **Step 3: 运行 `make test` 验证**

```bash
make test
```

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "chore: Makefile — auto submodule init + libbinding.a build"
```

### Task 9: 注释掉 Ollama 相关代码

**Files:**
- Modify: `internal/daemon/daemon.go` — 确认 Ollama provider 创建代码已删除
- Modify: `internal/daemon/routes.go` — 确认 Ollama URL 引用已删除
- No change: `internal/embedding/ollama.go` — 保留不动
- No change: `internal/embedding/ollama_test.go` — 保留不动

- [ ] **Step 1: 搜索所有 Ollama 引用**

```bash
rg -n "Ollama|ollama" --type go internal/
```

- [ ] **Step 2: 确认所有 Ollama 引用已从 daemon/ 和 config/ 中移除**

daemon 和 config 中不应再有对 Ollama 的引用。ollama.go 和 ollama_test.go 保留。

- [ ] **Step 3: 运行完整测试**

```bash
make test
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove Ollama references from daemon/config, keep ollama.go for future use"
```

### Task 10: 更新 memory.md 和 spec 文档

**Files:**
- Modify: `docs/01.memory.md`
- Modify: `docs/superpowers/specs/2026-04-19-daemon-config-ollama-design.md` — 标记部分内容为 superseded

- [ ] **Step 1: 更新 memory.md**

更新 Key Technical Decisions 中的 embedding 描述：

```
- **Embedding model**: `Qwen3-Embedding-0.6B-Q8_0.gguf` via llama-go (CGo 内嵌，daemon 进程内)
- **Embedding architecture**: tcpipuk/llama-go CGo bindings，按需加载 + 10min 空闲释放，Metal GPU 加速
- **HyDE**: HyDE query expansion via llama-go Generate (Qwen3-0.6B-Q8_0.gguf)，内嵌
```

- [ ] **Step 2: 更新旧 spec 标记**

在 `2026-04-19-daemon-config-ollama-design.md` 顶部添加：

```
> **Note:** Ollama provider 部分已被 `2026-04-20-llamacpp-embedded-design.md` 取代。
> Ollama 代码保留但当前不启用，默认使用 llama-go 内嵌推理。
```

- [ ] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs: update memory.md and mark old spec as superseded by llamacpp-embedded"
```

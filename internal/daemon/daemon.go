package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/got/loom"
	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

const (
	indexSyncInterval = 60 * time.Second // 索引轮询间隔
	embedTickInterval = 5 * time.Second  // embedding 轮询间隔
	embedTimeout      = 5 * time.Minute  // 背景嵌入操作超时
)

var PidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg         *config.Config
	server      *http.Server
	rebuildMu   sync.RWMutex
	wc          loom.WaitClose
	etaStartAt  atomic.Int64 // ETA 基准时间 (unix nano, 启动时记录)
	etaStartNum atomic.Int64 // ETA 基准已嵌入数 (启动时记录)

	tokenizer     tokenizer.Tokenizer
	indexer       *service.Indexer
	searcher      *service.Searcher
	embedder      *service.Embedder
	embedProvider embedding.EmbeddingProvider
	llmProvider   llm.LLMProvider
}

func NewDaemon(cfg *config.Config) *Daemon {
	return &Daemon{
		cfg: cfg,
	}
}

func (my *Daemon) Start(ctx context.Context) error {
	if err := acquireLockFile(); err != nil {
		return err
	}

	if err := dao.Init(my.cfg.Database.Path); err != nil {
		return fmt.Errorf("dao init failed: %w", err)
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		return fmt.Errorf("tokenizer init failed: %w", err)
	}

	my.tokenizer = tok
	my.embedProvider = newEmbedding(my.cfg)
	my.llmProvider = newLLM(my.cfg)
	if my.embedProvider == nil || my.llmProvider == nil {
		return fmt.Errorf("embedding or llm provider init failed")
	}

	my.indexer = service.NewIndexer(tok)
	my.searcher = service.NewSearcher(tok)
	my.embedder = service.NewEmbedder(my.embedProvider, my.cfg.Embedding.BatchSize, 0)

	handler := registerRoutes(my)
	mcp.RegisterHandler(my.handleToolCall)
	my.server = &http.Server{Handler: handler}

	addr := fmt.Sprintf(":%d", my.cfg.Daemon.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}

	loom.Go(func(later loom.Later) {
		if err := my.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logo.Error("daemon: serve error: %s", err)
		}
	})

	logo.Info("daemon: listening on %s", addr)
	loom.Go(my.goLoop)

	// ETA baseline: record embedded count at startup for average speed calculation
	_, embedded := dao.GetChunkCounts()
	my.etaStartAt.Store(time.Now().UnixNano())
	my.etaStartNum.Store(int64(embedded))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	signal.Ignore(syscall.SIGHUP)

	select {
	case <-sigCh:
		logo.Info("daemon: received signal, shutting down")
	case <-ctx.Done():
		logo.Info("daemon: context cancelled, shutting down")
	}

	my.Stop()
	os.Exit(0)
	return nil
}

func (my *Daemon) Stop() error {
	return my.wc.Close(func() error {
		if my.server != nil {
			my.server.Close()
		}

		if my.llmProvider != nil {
			my.llmProvider.Close()
		}

		if dao.DB != nil {
			store := dao.DB
			dao.DB = nil
			store.Close()
		}

		releaseLockFile()
		logo.Info("daemon: stopped")
		return nil
	})
}

func (my *Daemon) goLoop(later loom.Later) {
	summarizer := service.NewSummarizer(my.llmProvider, my.cfg.Summary, my.tokenizer, my.embedProvider)
	docIds := summarizer.ScanPendingDocs()
	closeChan := my.wc.C()

	pipelineTick := func() {
		newIds := my.syncIndex()
		docIds = append(docIds, newIds...)

		for _, id := range docIds {
			if err := summarizer.ProcessDoc(context.Background(), id); err != nil {
				logo.Warn("summarizer: process doc %d failed: %s", id, err)
			}
		}
		docIds = nil
	}

	var pipelineTicker = later.NewTicker(indexSyncInterval)
	var embedTicker = later.NewTicker(embedTickInterval)

	for {
		select {
		case <-closeChan:
			return
		case <-pipelineTicker.C:
			pipelineTick()
		case <-embedTicker.C:
			my.embedChunks()
		}
	}
}

func (my *Daemon) syncIndex() []int64 {
	my.rebuildMu.RLock()
	defer my.rebuildMu.RUnlock()
	return my.syncIndexUnlocked()
}

func (my *Daemon) syncIndexUnlocked() []int64 {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("indexPoller: list collections failed: %s", err)
		return nil
	}

	var dirtyIds []int64
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue // 系统 collection，不参与文件同步
		}
		result, err := my.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("indexPoller: %s failed: %s", col.Name, err)
			continue
		}
		if result != nil && len(result.DirtyDocIds) > 0 {
			dirtyIds = append(dirtyIds, result.DirtyDocIds...)
			logo.Info("indexPoller: %s found %d docs pending summary", col.Name, len(result.DirtyDocIds))
		}
		if result.Indexed > 0 || result.Updated > 0 || result.Removed > 0 {
			logo.Info("indexPoller: %s +%d ~%d -%d", col.Name, result.Indexed, result.Updated, result.Removed)
		}
	}
	return dirtyIds
}

func (my *Daemon) embedChunks() {
	count := dao.GetUnembeddedCount()
	if count == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), embedTimeout)
	defer cancel()

	// Cancel embedding context when daemon is shutting down
	var closeChan = my.wc.C()
	go func() {
		select {
		case <-closeChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	_, err := my.embedder.EmbedBatch(ctx, 0)
	if err != nil {
		logo.Error("embedChunks: %s", err)
	}
}

func IsRunning() bool {
	path := PidPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func StartBackground() error {
	if IsRunning() {
		return fmt.Errorf("daemon already running")
	}

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".cache", "lmd", "logs")
	os.MkdirAll(logDir, 0755)
	logFile, _ := os.OpenFile(filepath.Join(logDir, "daemon.stderr.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)

	cmd := exec.Command(os.Args[0], "daemon-start")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon failed: %w", err)
	}

	logFile.Close()
	cmd.Process.Release()
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	bts, err := convert.ToJsonE(v)
	if err != nil {
		w.Write([]byte(`{"error":"internal serialization error"}`))
		return
	}
	w.Write(bts)
}

func parseDuration(s string, defaultDuration time.Duration) time.Duration {
	if s == "" {
		return defaultDuration
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration
	}

	return dur
}

func newEmbedding(config *config.Config) embedding.EmbeddingProvider {
	var embedProv embedding.EmbeddingProvider
	switch config.Embedding.Provider {
	case "ollama":
		embedProv = embedding.NewOllamaProvider(config.Providers.Ollama.URL, config.Embedding.Model)
	case "siliconflow":
		embedProv = embedding.NewSiliconFlowEmbedding(config.Providers.SiliconFlow.URL, config.Embedding.Model, config.Providers.SiliconFlow.APIKey)
	default:
		logo.Error("unknown embedding provider: %s", config.Embedding.Provider)
		return nil
	}

	return embedProv
}

func newLLM(config *config.Config) llm.LLMProvider {
	var llmProv llm.LLMProvider
	switch config.Summary.Provider {
	case "ollama":
		llmProv = llm.NewOllamaLLM(config.Providers.Ollama.URL, config.Summary.Model, config.Summary.NoThinking)
	case "siliconflow":
		llmProv = llm.NewSiliconFlowLLM(config.Providers.SiliconFlow.URL, config.Summary.Model, config.Providers.SiliconFlow.APIKey)
	default:
		logo.Error("unknown summary provider: %s", config.Summary.Provider)
		return nil
	}

	return llmProv
}

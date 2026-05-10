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
	"strconv"
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
	indexSyncInterval     = 60 * time.Second // 索引轮询间隔
	embedTickInterval     = 5 * time.Second  // embedding 轮询间隔
	embedTimeout          = 5 * time.Minute  // 背景嵌入操作超时
)
var PidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg        *config.Config
	server     *http.Server
	rebuildMu  sync.RWMutex
	stopOnce   sync.Once
	stopCh     chan struct{}
	etaStartAt atomic.Int64 // ETA 基准时间 (unix nano, 启动时记录)
	etaStartNum atomic.Int64 // ETA 基准已嵌入数 (启动时记录)

	tokenizer     tokenizer.Tokenizer
	indexer       *service.Indexer
	searcher      *service.Searcher
	embedder      *service.Embedder
	embedProvider embedding.EmbeddingProvider
	llmProvider   llm.LLMProvider
	summarizer    *service.Summarizer
}

func NewDaemon(cfg *config.Config) *Daemon {
	return &Daemon{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

func (my *Daemon) Start(ctx context.Context) error {
	if err := acquireLock(); err != nil {
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

	var embedProv embedding.EmbeddingProvider
	switch my.cfg.Embedding.Provider {
	case "ollama":
		embedProv = embedding.NewOllamaProvider(my.cfg.Providers.Ollama.URL, my.cfg.Embedding.Model)
	case "siliconflow":
		embedProv = embedding.NewSiliconFlowEmbedding(my.cfg.Providers.SiliconFlow.URL, my.cfg.Embedding.Model, my.cfg.Providers.SiliconFlow.APIKey)
	default:
		return fmt.Errorf("unknown embedding provider: %s", my.cfg.Embedding.Provider)
	}
	my.embedProvider = embedProv

	var llmProv llm.LLMProvider
	switch my.cfg.Summary.Provider {
	case "ollama":
		llmProv = llm.NewOllamaLLM(my.cfg.Providers.Ollama.URL, my.cfg.Summary.Model, my.cfg.Summary.NoThinking)
	case "siliconflow":
		llmProv = llm.NewSiliconFlowLLM(my.cfg.Providers.SiliconFlow.URL, my.cfg.Summary.Model, my.cfg.Providers.SiliconFlow.APIKey)
	default:
		return fmt.Errorf("unknown summary provider: %s", my.cfg.Summary.Provider)
	}
	my.llmProvider = llmProv

	my.indexer = service.NewIndexer(tok)
	my.searcher = service.NewSearcher(tok)
	my.embedder = service.NewEmbedder(my.embedProvider, my.cfg.Embedding.BatchSize, 0)
	my.summarizer = service.NewSummarizer(my.llmProvider, my.cfg.Summary)

	handler := registerRoutes(my)
	mcp.RegisterHandler(my.handleToolCall)
	my.server = &http.Server{Handler: handler}

	port := my.cfg.Daemon.Port
	addr := fmt.Sprintf(":%d", port)
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

	return my.Stop()
}

func (my *Daemon) Stop() error {
	my.stopOnce.Do(func() {
		if my.stopCh != nil {
			select {
			case <-my.stopCh:
			default:
				close(my.stopCh)
			}
		}

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

		releaseLock()
		logo.Info("daemon: stopped")
	})
	return nil
}

func (my *Daemon) goLoop(later loom.Later) {
	var syncIndexTicker = later.NewTicker(indexSyncInterval)
	var embedTicker = later.NewTicker(embedTickInterval)

	summaryCooldown := my.cfg.Summary.CooldownSeconds
	if summaryCooldown <= 0 {
		summaryCooldown = 300
	}
	var summaryTicker = later.NewTicker(time.Duration(summaryCooldown) * time.Second)

	for {
		select {
		case <-my.stopCh:
			return
		case <-syncIndexTicker.C:
			my.syncIndex()
		case <-summaryTicker.C:
			my.summarizer.ProcessDirty()
		case <-embedTicker.C:
			my.embedChunks()
		}
	}
}

func (my *Daemon) syncIndex() {
	my.rebuildMu.RLock()
	defer my.rebuildMu.RUnlock()
	my.syncIndexUnlocked()
}

func (my *Daemon) syncIndexUnlocked() {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("indexPoller: list collections failed: %s", err)
		return
	}
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
			my.summarizer.MarkDirty(result.DirtyDocIds)
		}
		if result.Indexed > 0 || result.Updated > 0 || result.Removed > 0 {
			logo.Info("indexPoller: %s +%d ~%d -%d", col.Name, result.Indexed, result.Updated, result.Removed)
		}
	}
}

func (my *Daemon) embedChunks() {
	if dao.GetUnembeddedCount() == 0 {
		return
	}
	count := dao.GetUnembeddedCount()
	if count == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), embedTimeout)
	defer cancel()

	// Cancel embedding context when daemon is shutting down
	go func() {
		select {
		case <-my.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	_, err := my.embedder.EmbedBatch(ctx, 0)
	if err != nil {
		logo.Error("embedChunks: %s", err)
	}
}

var lockFile *os.File

func acquireLock() error {
	path := PidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("daemon already running (lock held on %s)", path)
	}

	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate pid file failed: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("seek pid file failed: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		return fmt.Errorf("write pid file failed: %w", err)
	}
	f.Sync()

	lockFile = f
	return nil
}

func releaseLock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		lockFile = nil
		os.Remove(PidPath())
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

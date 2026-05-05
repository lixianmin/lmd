package daemon

import (
	"context"
	"fmt"
	"io"
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
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

const (
	indexSyncInterval     = 60 * time.Second // 索引轮询间隔
	embedTickInterval     = 5 * time.Second  // embedding 轮询间隔
	daemonIdleTimeout     = 60 * time.Minute // daemon 空闲自动关闭超时
	serverShutdownTimeout = 5 * time.Second  // HTTP server 优雅关闭超时
	embedTimeout          = 5 * time.Minute  // 背景嵌入操作超时
)

var PidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg        *config.Config
	server     *http.Server
	wc         loom.WaitClose
	lastActive atomic.Int64
	rebuildMu  sync.RWMutex
	stopOnce   sync.Once
	stopCh     chan struct{}
	goLoopWg   sync.WaitGroup
	etaStartAt atomic.Int64 // ETA 基准时间 (unix nano, 启动时记录)
	etaStartNum atomic.Int64 // ETA 基准已嵌入数 (启动时记录)

	tokenizer  tokenizer.Tokenizer
	indexer    *service.Indexer
	searcher   *service.Searcher
	embedder   *service.Embedder
	provider   *embedding.LlamaProvider
	memSvc     *service.MemoryService
	hydeClient *service.HyDEAPIClient
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

	embedURLs := []string{
		"https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf",
		"https://hf-mirror.com/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf",
	}
	fmt.Fprintf(os.Stderr, "  Checking embedding model...\n")
	if err := service.DownloadModel(my.cfg.Llama.EmbedModel, embedURLs...); err != nil {
		logo.Warn("daemon: embed model download failed: %s (will retry on first use)", err)
		fmt.Fprintf(os.Stderr, "  Warning: embedding model download failed: %s\n", err)
	}

	my.provider = embedding.NewLlamaProvider(
		my.cfg.Llama.EmbedModel,
		my.cfg.Llama.GPULayers,
		my.cfg.Llama.Threads,
		my.cfg.Llama.Parallel,
	)

	my.indexer = service.NewIndexer(tok)
	my.searcher = service.NewSearcher(tok)
	my.embedder = service.NewEmbedder(my.provider, my.cfg.Embedding.BatchSize, my.cfg.Embedding.Truncation)
	my.memSvc = service.NewMemoryService()

	my.hydeClient = service.NewHyDEAPIClient(
		my.cfg.HyDE.BaseURL, my.cfg.HyDE.APIKey, my.cfg.HyDE.Model, my.cfg.HyDE.MaxTokens,
	)

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
	my.touchActivity()
	my.goLoopWg.Add(1)
	loom.Go(my.goLoop)

	// ETA baseline: record embedded count at startup for average speed calculation
	_, embedded := dao.GetChunkCounts()
	my.etaStartAt.Store(time.Now().UnixNano())
	my.etaStartNum.Store(int64(embedded))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		logo.Info("daemon: received signal, shutting down")
	case <-ctx.Done():
		logo.Info("daemon: context cancelled, shutting down")
	case <-my.stopCh:
		logo.Info("daemon: idle timeout, shutting down")
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
			defer cancel()
			my.server.Shutdown(shutdownCtx)
		}

		my.wc.Close(func() error {
			return nil
		})
		my.goLoopWg.Wait()

		if dao.DB != nil {
			store := dao.DB
			dao.DB = nil
			store.Close()
		}

		if my.provider != nil {
			my.provider.Close()
		}

		releaseLock()
		logo.Info("daemon: stopped")
	})
	return nil
}

func (my *Daemon) touchActivity() {
	my.lastActive.Store(time.Now().UnixNano())
}

func (my *Daemon) goLoop(later loom.Later) {
	defer my.goLoopWg.Done()

	var syncIndexTicker = later.NewTicker(indexSyncInterval)
	var embedTicker = later.NewTicker(embedTickInterval)
	var modelIdleTimeout = parseDuration(my.cfg.Llama.ModelIdleTimeout, 10*time.Minute)
	var idleTimeout = daemonIdleTimeout

	var closeChan = my.wc.C()
	for {
		select {
		case <-closeChan:
			return
		case <-syncIndexTicker.C:
			my.syncIndex()
		case <-embedTicker.C:
			my.embedChunks()
			my.provider.ReleaseIfIdle(modelIdleTimeout)

			last := time.Unix(0, my.lastActive.Load())
			if !last.IsZero() && time.Since(last) > idleTimeout {
				logo.Info("daemon: idle timeout reached, shutting down")
				loom.Go(func(later loom.Later) {
					my.Stop()
				})
				return
			}
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
			continue // 系统 collection，由 memory 接口管理，不参与文件同步
		}
		result, err := my.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("indexPoller: %s failed: %s", col.Name, err)
			continue
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

	stderrR, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe failed: %w", err)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon failed: %w", err)
	}

	loom.Go(func(later loom.Later) {
		io.Copy(os.Stderr, stderrR)
	})

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

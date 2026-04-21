package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/lixianmin/got/loom"
	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

var PidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg        *config.Config
	server     *http.Server
	wc         loom.WaitClose
	lastActive time.Time
	rebuildMu  sync.Mutex
	stopOnce   sync.Once

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
		cfg: cfg,
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
	my.memSvc = service.NewMemoryService(tok)

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
		my.server.Serve(listener)
	})
	logo.Info("daemon: listening on %s", addr)
	my.touchActivity()
	loom.Go(my.goLoop)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

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
		if my.server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			my.server.Shutdown(shutdownCtx)
		}

		if dao.DB != nil {
			dao.DB.Close()
		}

		if my.provider != nil {
			my.provider.Close()
		}

		my.wc.Close(func() error {
			return nil
		})

		releaseLock()
		logo.Info("daemon: stopped")
	})
	return nil
}

func (my *Daemon) touchActivity() {
	my.lastActive = time.Now()
}

func (my *Daemon) goLoop(later loom.Later) {
	var syncIndexTicker = later.NewTicker(60 * time.Second)
	var embedTicker = later.NewTicker(5 * time.Second)
	var modelIdleTimeout = parseDuration(my.cfg.Llama.ModelIdleTimeout, 10*time.Minute)
	var idleTimeout = 60 * time.Minute

	var closeChan = my.wc.C()
	for {
		select {
		case <-closeChan:
			return
		case <-syncIndexTicker.C:
			my.syncIndex()
		case <-embedTicker.C:
			my.embedChunks()
			my.embedMemories()
			my.provider.ReleaseIfIdle(modelIdleTimeout)

			if !my.lastActive.IsZero() && time.Since(my.lastActive) > idleTimeout {
				logo.Info("daemon: idle timeout reached, shutting down")
				my.Stop()
				return
			}
		}
	}
}

func (my *Daemon) syncIndex() {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("indexPoller: list collections failed: %s", err)
		return
	}
	for _, col := range cols {
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
	my.rebuildMu.Lock()
	defer my.rebuildMu.Unlock()

	count := dao.GetUnembeddedCount()
	if count == 0 {
		return
	}

	_, err := my.embedder.EmbedBatch(context.Background(), 0)
	if err != nil {
		logo.Error("embedChunks: %s", err)
	}
}

func (my *Daemon) embedMemories() {
	my.rebuildMu.Lock()
	defer my.rebuildMu.Unlock()

	count := dao.GetUnembeddedMemoryCount()
	if count == 0 {
		return
	}
	memories, err := dao.GetUnembeddedMemories(8)
	if err != nil || len(memories) == 0 {
		return
	}

	texts := make([]string, len(memories))
	for i, m := range memories {
		texts[i] = m.Content
	}

	vecs, err := my.provider.EmbedBatch(context.Background(), texts)
	if err != nil {
		logo.Error("embedMemories: %s", err)
		return
	}

	for i, vec := range vecs {
		blob, err := sqlite_vec.SerializeFloat32(vec)
		if err != nil {
			continue
		}
		dao.UpdateMemoryEmbedding(memories[i].ID, blob)
	}
	logo.Info("embedMemories: embedded=%d", len(vecs))
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

	f.Truncate(0)
	f.Seek(0, 0)
	f.WriteString(strconv.Itoa(os.Getpid()))
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

	stderrR, _ := cmd.StderrPipe()

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon failed: %w", err)
	}

	loom.Go(func(later loom.Later) {
		io.Copy(os.Stderr, stderrR)
	})

	cmd.Process.Release()
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
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

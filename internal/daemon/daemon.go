package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

var pidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg        *config.Config
	server     *http.Server
	done       chan struct{}
	lastActive time.Time

	tokenizer tokenizer.Tokenizer
	indexer   *service.Indexer
	searcher  *service.Searcher
	embedder  *service.Embedder
	provider  embedding.EmbeddingProvider
}

func NewDaemon(cfg *config.Config) *Daemon {
	return &Daemon{
		cfg:  cfg,
		done: make(chan struct{}),
	}
}

func (my *Daemon) Start(ctx context.Context) error {
	if err := dao.Init(my.cfg.Database.Path); err != nil {
		return fmt.Errorf("dao init failed: %w", err)
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		return fmt.Errorf("tokenizer init failed: %w", err)
	}
	my.tokenizer = tok

	my.provider = embedding.NewOllamaProvider(
		my.cfg.Embedding.Ollama.URL,
		my.cfg.Embedding.Ollama.Model,
	)
	my.indexer = service.NewIndexer(tok)
	my.searcher = service.NewSearcher(tok)
	my.embedder = service.NewEmbedder(my.provider)

	handler := registerRoutes(my)
	my.server = &http.Server{Handler: handler}

	port := my.cfg.Daemon.Port
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}

	if err := writePid(); err != nil {
		logo.Warn("writePid failed: %s", err)
	}

	go my.server.Serve(listener)
	logo.Info("daemon: listening on %s", addr)
	my.touchActivity()

	idleTimeout, _ := time.ParseDuration(my.cfg.Daemon.IdleTimeout)
	if idleTimeout == 0 {
		idleTimeout = 30 * time.Minute
	}
	pollInterval, _ := time.ParseDuration(my.cfg.Daemon.IndexPollInterval)
	if pollInterval == 0 {
		pollInterval = 60 * time.Second
	}

	go my.indexPoller(pollInterval)
	go my.embedWorker()
	go my.idleMonitor(idleTimeout)

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
	if my.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		my.server.Shutdown(shutdownCtx)
	}
	if dao.DB != nil {
		dao.DB.Close()
	}
	select {
	case <-my.done:
	default:
		close(my.done)
	}
	pidFilePath := pidPath()
	os.Remove(pidFilePath)
	logo.Info("daemon: stopped")
	return nil
}

func (my *Daemon) touchActivity() {
	my.lastActive = time.Now()
}

func (my *Daemon) indexPoller(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-my.done:
			return
		case <-ticker.C:
			my.syncIndex()
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

func (my *Daemon) embedWorker() {
	for {
		select {
		case <-my.done:
			return
		default:
			count := dao.GetUnembeddedCount()
			if count == 0 {
				time.Sleep(10 * time.Second)
				continue
			}
			_, err := my.embedder.EmbedBatch(context.Background(), 0)
			if err != nil {
				logo.Error("embedWorker: %s", err)
				time.Sleep(30 * time.Second)
			}
		}
	}
}

func (my *Daemon) idleMonitor(timeout time.Duration) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-my.done:
			return
		case <-ticker.C:
			if !my.lastActive.IsZero() && time.Since(my.lastActive) > timeout {
				logo.Info("daemon: idle timeout reached, shutting down")
				my.Stop()
				return
			}
		}
	}
}

func writePid() error {
	path := pidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func readPid() (int, error) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func IsRunning() bool {
	pid, err := readPid()
	if err != nil {
		return false
	}
	if !isProcessAlive(pid) {
		return false
	}
	cfg := config.Cfg
	if cfg == nil {
		cfg, _ = config.Load()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	client := NewClient(cfg.Daemon.Port)
	return client.IsAlive()
}

func StartBackground() error {
	cmd := exec.Command(os.Args[0], "daemon", "--detach")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon failed: %w", err)
	}
	cmd.Process.Release()

	cfg := config.Cfg
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	client := NewClient(cfg.Daemon.Port)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if client.IsAlive() {
			return nil
		}
	}
	return fmt.Errorf("daemon did not become ready within 30s")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

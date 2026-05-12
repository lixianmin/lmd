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

var PidPath = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lmd", "daemon.pid")
}

type Daemon struct {
	cfg         *config.Config
	server      *http.Server
	wc          loom.WaitClose
	etaStartAt  atomic.Int64
	etaStartNum atomic.Int64

	tokenizer     tokenizer.Tokenizer
	chunkIndexer  *service.ChunkIndexer
	hydeIndexer   *service.HyDEIndexer
	searcher      *service.Searcher
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

	my.chunkIndexer = service.NewChunkIndexer(tok, my.embedProvider)
	my.hydeIndexer = service.NewHyDEIndexer(my.llmProvider, my.embedProvider, tok, my.cfg.Hyde)
	my.searcher = service.NewSearcher(tok)

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
	loom.Go(my.goChunkLoop)
	loom.Go(my.goHydeLoop)

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

func (my *Daemon) goChunkLoop(later loom.Later) {
	closeChan := my.wc.C()
	my.runChunkPipeline()

	ticker := later.NewTicker(60 * time.Second)
	for {
		select {
		case <-closeChan:
			return
		case <-ticker.C:
			my.runChunkPipeline()
		}
	}
}

func (my *Daemon) goHydeLoop(later loom.Later) {
	closeChan := my.wc.C()
	my.runHydePipeline()

	ticker := later.NewTicker(60 * time.Second)
	for {
		select {
		case <-closeChan:
			return
		case <-ticker.C:
			my.runHydePipeline()
		}
	}
}

func (my *Daemon) runChunkPipeline() {
	pending := my.scanDocChanges()
	if len(pending) == 0 {
		return
	}

	total := len(pending)
	dao.SetMeta("pipeline.status", "running")
	dao.SetMeta("pipeline.total", fmt.Sprintf("%d", total))

	closeChan := my.wc.C()
	var errors int
	for i, doc := range pending {
		select {
		case <-closeChan:
			dao.SetMeta("pipeline.status", "idle")
			return
		default:
		}
		if err := my.chunkIndexer.ProcessDoc(context.Background(), doc); err != nil {
			errors++
			logo.Warn("chunkPipeline: process %s/%s failed: %s", doc.Collection, doc.Path, err)
		}
		if (i+1)%50 == 0 {
			dao.SetMeta("pipeline.processed", fmt.Sprintf("%d", i+1))
		}
	}

	dao.SetMeta("pipeline.status", "idle")
	dao.SetMeta("pipeline.processed", fmt.Sprintf("%d", total))
	if errors > 0 {
		logo.Warn("chunkPipeline: processed %d docs, errors=%d", total, errors)
	}
}

func (my *Daemon) runHydePipeline() {
	docs, err := dao.FindDocsWithMissingHydeData(100)
	if err != nil {
		logo.Warn("hydePipeline: find docs failed: %s", err)
		return
	}
	if len(docs) == 0 {
		return
	}

	cols, err := dao.ListCollections()
	if err != nil {
		logo.Warn("hydePipeline: list collections failed: %s", err)
		return
	}
	colMap := make(map[string]dao.CollectionRecord, len(cols))
	for _, c := range cols {
		colMap[c.Name] = c
	}

	logo.Info("hydePipeline: found %d docs with missing hyde data", len(docs))
	closeChan := my.wc.C()

	var errors int
	for _, doc := range docs {
		select {
		case <-closeChan:
			return
		default:
		}
		col, ok := colMap[doc.Collection]
		if !ok {
			continue
		}
		pending := service.PendingDoc{
			Action:     service.DocNew,
			Collection: doc.Collection,
			RootDir:    col.Path,
			Path:       doc.Path,
			Title:      doc.Title,
			Body:       doc.Body,
			Hash:       doc.Hash,
			FileSize:   doc.FileSize,
		}
		if err := my.hydeIndexer.ProcessDoc(context.Background(), pending); err != nil {
			errors++
			logo.Warn("hydePipeline: process %s/%s failed: %s", doc.Collection, doc.Path, err)
		}
	}
	if errors > 0 {
		logo.Warn("hydePipeline: processed %d docs, errors=%d", len(docs), errors)
	}
}

func (my *Daemon) scanDocChanges() []service.PendingDoc {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Error("pipeline: list collections failed: %s", err)
		return nil
	}

	var pending []service.PendingDoc
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue
		}
		result, err := my.chunkIndexer.ScanChanges(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("pipeline: scan %s failed: %s", col.Name, err)
			continue
		}
		if len(result) > 0 {
			logo.Info("pipeline: %s found %d pending docs", col.Name, len(result))
		}
		pending = append(pending, result...)
	}

	if len(pending) == 0 {
		incomplete, err := my.chunkIndexer.ScanIncomplete(100)
		if err != nil {
			logo.Error("pipeline: scan incomplete failed: %s", err)
		} else if len(incomplete) > 0 {
			logo.Info("pipeline: found %d docs with missing embeddings", len(incomplete))
			pending = incomplete
		}
	}

	return pending
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
		embedProv = embedding.NewOllamaProvider(config.Providers.Ollama.BaseURL, config.Embedding.Model, config.Embedding.QueryPrefix)
	case "siliconflow":
		embedProv = embedding.NewSiliconFlowEmbedding(config.Providers.SiliconFlow.BaseURL, config.Embedding.Model, config.Providers.SiliconFlow.APIKey, config.Embedding.QueryPrefix)
	default:
		logo.Error("unknown embedding provider: %s", config.Embedding.Provider)
		return nil
	}

	return embedProv
}

func newLLM(config *config.Config) llm.LLMProvider {
	var llmProv llm.LLMProvider
	switch config.Hyde.Provider {
	case "ollama":
		llmProv = llm.NewOllamaLLM(config.Providers.Ollama.BaseURL, config.Hyde.Model, config.Hyde.NoThinking)
	case "siliconflow":
		llmProv = llm.NewSiliconFlowLLM(config.Providers.SiliconFlow.BaseURL, config.Hyde.Model, config.Providers.SiliconFlow.APIKey)
	case "deepseek":
		llmProv = llm.NewSiliconFlowLLM(config.Providers.DeepSeek.BaseURL, config.Hyde.Model, config.Providers.DeepSeek.APIKey)
	default:
		logo.Error("unknown hyde provider: %s", config.Hyde.Provider)
		return nil
	}

	return llmProv
}

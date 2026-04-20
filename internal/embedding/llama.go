package embedding

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/lixianmin/logo"
	llama "github.com/tcpipuk/llama-go"
)

type LlamaProvider struct {
	modelPath string
	gpuLayers int
	threads   int
	parallel  int
	dim       int

	mu         sync.Mutex
	model      *llama.Model
	lctx       *llama.Context
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

	lctx, err := model.NewContext(
		llama.WithEmbeddings(),
		llama.WithThreads(my.threads),
		llama.WithParallel(my.parallel),
	)
	if err != nil {
		model.Close()
		return fmt.Errorf("create embedding context failed: %w", err)
	}

	my.model = model
	my.lctx = lctx
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

	vecs, err := my.lctx.GetEmbeddingsBatch(texts)
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

	if my.lctx != nil {
		my.lctx.Close()
		my.lctx = nil
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
		my.lctx.Close()
		my.lctx = nil
		my.model.Close()
		my.model = nil
		logo.Info("LlamaProvider: model released after idle %s", timeout)
		return true
	}
	return false
}

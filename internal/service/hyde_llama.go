package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lixianmin/logo"
	llama "github.com/tcpipuk/llama-go"
)

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

func (my *LlamaHyDEModel) Generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if err := my.ensureLoaded(); err != nil {
		return "", err
	}

	my.mu.Lock()
	defer my.mu.Unlock()

	lctx, err := my.model.NewContext(
		llama.WithContext(2048),
		llama.WithThreads(my.threads),
	)
	if err != nil {
		return "", fmt.Errorf("hyde create context failed: %w", err)
	}
	defer lctx.Close()

	text, err := lctx.Generate(prompt, llama.WithMaxTokens(maxTokens))
	if err != nil {
		return "", fmt.Errorf("hyde generate failed: %w", err)
	}

	my.lastActive = time.Now()
	return strings.TrimSpace(text), nil
}

func (my *LlamaHyDEModel) ensureLoaded() error {
	my.mu.Lock()
	defer my.mu.Unlock()

	if my.model != nil {
		return nil
	}
	if _, err := os.Stat(my.modelPath); os.IsNotExist(err) {
		return fmt.Errorf("hyde model not found: %s", my.modelPath)
	}

	model, err := llama.LoadModel(my.modelPath, llama.WithGPULayers(my.gpuLayers))
	if err != nil {
		return fmt.Errorf("hyde load model failed: %w", err)
	}
	my.model = model
	my.lastActive = time.Now()
	logo.Info("LlamaHyDEModel: loaded %s", my.modelPath)
	return nil
}

func (my *LlamaHyDEModel) ReleaseIfIdle(timeout time.Duration) bool {
	my.mu.Lock()
	defer my.mu.Unlock()
	if my.model == nil {
		return false
	}
	if time.Since(my.lastActive) > timeout {
		my.model.Close()
		my.model = nil
		logo.Info("LlamaHyDEModel: released after idle %s", timeout)
		return true
	}
	return false
}

func (my *LlamaHyDEModel) Close() error {
	my.mu.Lock()
	defer my.mu.Unlock()
	if my.model != nil {
		my.model.Close()
		my.model = nil
	}
	return nil
}

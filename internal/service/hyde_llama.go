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

const hydePromptTemplate = "/no_think Write a brief factual passage (50-150 words) that directly answers this question. Use only relevant facts and terminology.\n\nQuestion: %s"

type HyDEModel interface {
	Generate(ctx context.Context, prompt string, maxTokens int) (string, error)
}

type HyDEGenerator struct {
	model     HyDEModel
	maxTokens int
}

func NewHyDEGenerator(model HyDEModel, maxTokens int) *HyDEGenerator {
	return &HyDEGenerator{model: model, maxTokens: maxTokens}
}

func (g *HyDEGenerator) Generate(ctx context.Context, query string) (string, error) {
	prompt := fmt.Sprintf(hydePromptTemplate, query)
	return g.model.Generate(ctx, prompt, g.maxTokens)
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
	my.mu.Lock()
	defer my.mu.Unlock()

	if err := my.loadLocked(); err != nil {
		return "", err
	}

	t0 := time.Now()
	lctx, err := my.model.NewContext(
		llama.WithContext(4096),
		llama.WithThreads(my.threads),
	)
	if err != nil {
		return "", fmt.Errorf("hyde create context failed: %w", err)
	}
	defer lctx.Close()

	text, err := lctx.Generate(prompt, llama.WithMaxTokens(maxTokens), llama.WithTemperature(0.0))
	if err != nil {
		return "", fmt.Errorf("hyde generate failed: %w", err)
	}

	result := strings.TrimSpace(text)
	my.lastActive = time.Now()
	logo.Info("LlamaHyDEModel: generate done (%s): %s", time.Since(t0), truncateString(result, 300))
	return result, nil
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

func (my *LlamaHyDEModel) loadLocked() error {
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

func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

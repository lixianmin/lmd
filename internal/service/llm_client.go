package service

import (
	"fmt"
	"os"
	"sync"

	llama "github.com/tcpipuk/llama-go"
	"github.com/lixianmin/logo"
)

type LLMClient struct {
	modelPath string
	gpuLayers int
	threads   int

	mu    sync.Mutex
	model *llama.Model
	lctx  *llama.Context
}

func NewLLMClient(modelPath string, gpuLayers, threads int) (*LLMClient, error) {
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("summarize model not found: %s", modelPath)
	}
	return &LLMClient{
		modelPath: modelPath,
		gpuLayers: gpuLayers,
		threads:   threads,
	}, nil
}

func (my *LLMClient) Generate(prompt string, maxTokens int) (string, error) {
	my.mu.Lock()
	defer my.mu.Unlock()

	if err := my.loadLocked(); err != nil {
		return "", err
	}

	// Use Qwen3 chat template
	fullPrompt := fmt.Sprintf("<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n", prompt)

	output, err := my.lctx.Generate(fullPrompt,
		llama.WithMaxTokens(maxTokens),
	)
	if err != nil {
		return "", fmt.Errorf("generate failed: %w", err)
	}

	return output, nil
}

func (my *LLMClient) loadLocked() error {
	if my.model != nil {
		return nil
	}

	model, err := llama.LoadModel(my.modelPath, llama.WithGPULayers(my.gpuLayers))
	if err != nil {
		return fmt.Errorf("load summarize model failed: %w", err)
	}

	// Create context without Embeddings flag since this is a generation model
	lctx, err := model.NewContext(
		llama.WithThreads(my.threads),
	)
	if err != nil {
		model.Close()
		return fmt.Errorf("create context failed: %w", err)
	}

	my.model = model
	my.lctx = lctx
	logo.Info("LLMClient: model loaded from %s", my.modelPath)
	return nil
}

func (my *LLMClient) Close() error {
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
	return nil
}

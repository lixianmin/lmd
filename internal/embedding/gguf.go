package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const defaultModelFilename = "Qwen3-Embedding-0.6B-Q8_0.gguf"

type GGUFProvider struct {
	modelPath string
	dim       int
	baseURL   string
	cmd       *exec.Cmd
	mu        sync.Mutex
	started   bool
}

func NewGGUFProvider(modelPath string) *GGUFProvider {
	return &GGUFProvider{
		modelPath: modelPath,
		dim:       1024,
		baseURL:   "http://127.0.0.1:61999",
	}
}

func DefaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "lmd", "models", defaultModelFilename)
}

func (g *GGUFProvider) ensureServer() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.started {
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:61999")
	if err == nil {
		ln.Close()
	} else {
		resp, err := http.Get(g.baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			g.started = true
			return nil
		}
	}

	cmd := exec.Command("llama-server",
		"-m", g.modelPath,
		"--pooling", "mean",
		"-ngl", "99",
		"-t", "4",
		"--port", "61999",
		"--host", "127.0.0.1",
		"--embedding",
		"--log-disable",
		"-b", "2048",
		"--ubatch-size", "2048",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-server: %w", err)
	}
	g.cmd = cmd

	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get(g.baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			g.started = true
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return fmt.Errorf("llama-server failed to start within 30s")
}

type embeddingRequest struct {
	Input interface{} `json:"input"`
	Model string      `json:"model"`
}

type embeddingItem struct {
	Index     int         `json:"index"`
	Embedding [][]float32 `json:"embedding"`
}

func (g *GGUFProvider) callEmbedAPI(input interface{}) ([][]float32, error) {
	if err := g.ensureServer(); err != nil {
		return nil, err
	}

	body, _ := json.Marshal(embeddingRequest{Input: input, Model: "default"})
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(g.baseURL+"/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API returned %d: %s", resp.StatusCode, string(b))
	}

	var items []embeddingItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	vecs := make([][]float32, len(items))
	for _, item := range items {
		if len(item.Embedding) > 0 {
			vecs[item.Index] = item.Embedding[0]
		}
	}
	return vecs, nil
}

func (g *GGUFProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := g.callEmbedAPI(text)
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (g *GGUFProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return g.callEmbedAPI(texts)
}

func (g *GGUFProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return g.Embed(ctx, query)
}

func (g *GGUFProvider) Dimension() int    { return g.dim }
func (g *GGUFProvider) ModelName() string { return "Qwen3-Embedding-0.6B-Q8_0" }
func (g *GGUFProvider) Close() error {
	if g.cmd != nil && g.cmd.Process != nil {
		g.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() {
			done <- g.cmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			g.cmd.Process.Kill()
		}
	}
	return nil
}

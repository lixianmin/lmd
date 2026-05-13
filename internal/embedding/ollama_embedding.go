package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ollamaHTTPTimeout = 120 * time.Second // Ollama HTTP 客户端超时

type OllamaEmbedding struct {
	baseURL     string
	model       string
	queryPrefix string
	client      *http.Client
}

func NewOllamaProvider(url, model, queryPrefix string) *OllamaEmbedding {
	return &OllamaEmbedding{
		baseURL:     url,
		model:       model,
		queryPrefix: queryPrefix,
		client:      &http.Client{Timeout: ollamaHTTPTimeout},
	}
}

func (my *OllamaEmbedding) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := my.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vecs[0], nil
}

func (my *OllamaEmbedding) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload := map[string]any{
		"model": my.model,
		"input": texts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama embed marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", my.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := my.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode failed: %w", err)
	}
	return result.Embeddings, nil
}

func (my *OllamaEmbedding) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, my.queryPrefix+query)
}

func (my *OllamaEmbedding) Dimension() int { return EmbeddingDim }

func (my *OllamaEmbedding) ModelName() string { return my.model }

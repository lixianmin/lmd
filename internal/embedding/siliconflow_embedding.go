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

type SiliconFlowEmbedding struct {
	baseURL     string
	model       string
	apiKey      string
	queryPrefix string
	client      *http.Client
}

func NewSiliconFlowEmbedding(url, model, apiKey, queryPrefix string) *SiliconFlowEmbedding {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &SiliconFlowEmbedding{
		baseURL:     url,
		model:       model,
		apiKey:      apiKey,
		queryPrefix: queryPrefix,
		client:      &http.Client{Timeout: 120 * time.Second},
	}
}

type sfEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type sfEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (my *SiliconFlowEmbedding) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := sfEmbedRequest{Model: my.model, Input: texts}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("siliconflow embed marshal: %w", err)
	}

	url := fmt.Sprintf("%s/embeddings", my.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+my.apiKey)

	resp, err := my.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("siliconflow embed: %s %s", resp.Status, string(respBytes))
	}

	var result sfEmbedResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, err
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}
	return embeddings, nil
}

func (my *SiliconFlowEmbedding) Embed(ctx context.Context, text string) ([]float32, error) {
	batch, err := my.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return batch[0], nil
}

func (my *SiliconFlowEmbedding) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, my.queryPrefix+query)
}

func (my *SiliconFlowEmbedding) Dimension() int {
	return EmbeddingDim
}

func (my *SiliconFlowEmbedding) ModelName() string {
	return my.model
}

func (my *SiliconFlowEmbedding) Close() error {
	return nil
}

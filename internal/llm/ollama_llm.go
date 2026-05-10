package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ollamaLLMHTTPTimeout = 120 * time.Second // Ollama LLM HTTP 客户端超时

type OllamaLLM struct {
	baseURL    string
	model      string
	client     *http.Client
	noThinking bool
}

func NewOllamaLLM(url, model string, noThinking bool) *OllamaLLM {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &OllamaLLM{
		baseURL:    url,
		model:      model,
		client:     &http.Client{Timeout: ollamaLLMHTTPTimeout},
		noThinking: noThinking,
	}
}

type ollamaChatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (my *OllamaLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	reqBody := ollamaChatRequest{
		Model:    my.model,
		Messages: messages,
		Stream:   false,
	}
	if my.noThinking {
		reqBody.Options = map[string]any{"enable_thinking": false}
	}

	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", my.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := my.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama chat returned %d: %s", resp.StatusCode, string(respBytes))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama chat read failed: %w", err)
	}

	var result ollamaChatResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", fmt.Errorf("ollama chat decode failed: %w", err)
	}

	return result.Message.Content, nil
}

func (my *OllamaLLM) Close() error {
	return nil
}

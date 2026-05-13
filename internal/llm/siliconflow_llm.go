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

type SiliconFlowLLM struct {
	baseURL   string
	model     string
	apiKey    string
	maxTokens int
	client    *http.Client
}

func NewSiliconFlowLLM(url, model, apiKey string) *SiliconFlowLLM {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return &SiliconFlowLLM{
		baseURL:   url,
		model:     model,
		apiKey:    apiKey,
		maxTokens: 512,
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

type sfChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type sfChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (my *SiliconFlowLLM) ChatCompletion(ctx context.Context, messages []Message) (string, error) {
	reqBody := sfChatRequest{
		Model:       my.model,
		Messages:    messages,
		MaxTokens:   my.maxTokens,
		Temperature: 0.3,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("siliconflow marshal: %w", err)
	}
	url := fmt.Sprintf("%s/chat/completions", my.baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+my.apiKey)

	resp, err := my.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("siliconflow chat: %s %s", resp.Status, string(respBytes))
	}

	var result sfChatResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("siliconflow chat: no choices returned")
	}

	return result.Choices[0].Message.Content, nil
}

func (my *SiliconFlowLLM) Close() error {
	return nil
}

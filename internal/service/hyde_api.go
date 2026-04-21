package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lixianmin/logo"
)

const hydePromptTemplate = "/no_think Write a brief factual passage (50-150 words) that directly answers this question. Use only relevant facts and terminology.\n\nQuestion: %s"

type HyDEAPIClient struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func NewHyDEAPIClient(baseURL, apiKey, model string, maxTokens int) *HyDEAPIClient {
	return &HyDEAPIClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (my *HyDEAPIClient) Generate(ctx context.Context, query string) (string, error) {
	if my.apiKey == "" {
		return "", fmt.Errorf("HyDE requires api_key, set hyde.api_key in config")
	}

	prompt := fmt.Sprintf(hydePromptTemplate, query)

	payload := map[string]interface{}{
		"model": my.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": my.maxTokens,
		"stream":     false,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, my.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("hyde create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+my.apiKey)

	t0 := time.Now()
	resp, err := my.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hyde request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("hyde read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hyde API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("hyde decode failed: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("hyde API returned no choices")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	logo.Info("HyDEAPIClient: generate done (%s): %s", time.Since(t0), truncateString(content, 300))
	return content, nil
}

func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

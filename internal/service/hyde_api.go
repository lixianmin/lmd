package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/got/webx"
	"github.com/lixianmin/logo"
)

const hydePromptTemplate = "Write a brief factual passage (50-150 words) that directly answers this question. Use only relevant facts and terminology.\n\nQuestion: %s"

const hydeTimeout = 60 * time.Second

const (
	hydePromptLogMaxRunes   = 500  // HyDE prompt 日志截断 rune 数
	hydeResponseLogMaxRunes = 1000 // HyDE 原始响应日志截断 rune 数
	hydeContentLogMaxRunes  = 300  // HyDE 提取内容日志截断 rune 数
)

type HyDEAPIClient struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
}

func NewHyDEAPIClient(baseURL, apiKey, model string, maxTokens int) *HyDEAPIClient {
	return &HyDEAPIClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
	}
}

func (my *HyDEAPIClient) Generate(ctx context.Context, query string) (string, error) {
	if my.apiKey == "" {
		return "", fmt.Errorf("HyDE requires api_key, set hyde.api_key in config")
	}

	ctx, cancel := context.WithTimeout(ctx, hydeTimeout)
	defer cancel()

	prompt := fmt.Sprintf(hydePromptTemplate, query)
	logo.Info("HyDEAPIClient: prompt: %s", truncateString(prompt, hydePromptLogMaxRunes))

	t0 := time.Now()
	respBody, err := my.doRequest(ctx, prompt)
	if err != nil {
		return "", err
	}
	logo.Info("HyDEAPIClient: raw response (%s): %s", time.Since(t0), truncateString(string(respBody), hydeResponseLogMaxRunes))

	content, err := my.extractContent(respBody)
	if err != nil {
		return "", err
	}

	logo.Info("HyDEAPIClient: done (%s): %s", time.Since(t0), truncateString(content, hydeContentLogMaxRunes))
	return content, nil
}

func (my *HyDEAPIClient) doRequest(ctx context.Context, prompt string) ([]byte, error) {
	payload := map[string]any{
		"model": my.model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"max_tokens":      my.maxTokens,
		"stream":          false,
		"enable_thinking": false,
	}

	body, err := convert.ToJsonE(payload)
	if err != nil {
		return nil, fmt.Errorf("hyde marshal failed: %w", err)
	}
	respBody, err := webx.Post(ctx, my.baseURL+"/chat/completions", webx.WithRequestBuilder(func(req *http.Request) string {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+my.apiKey)
		return convert.String(body)
	}))
	if err != nil {
		return nil, fmt.Errorf("hyde request failed: %w", err)
	}

	return respBody, nil
}

func (my *HyDEAPIClient) extractContent(data []byte) (string, error) {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if convert.FromJsonE(data, &errResp) == nil && errResp.Error.Message != "" {
		return "", fmt.Errorf("hyde API error: %s", errResp.Error.Message)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := convert.FromJsonE(data, &result); err != nil {
		return "", fmt.Errorf("hyde decode failed: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("hyde API returned no choices")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if content == "" {
		content = strings.TrimSpace(result.Choices[0].Message.ReasoningContent)
	}
	if content == "" {
		return "", fmt.Errorf("hyde API returned empty content")
	}

	return content, nil
}

func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

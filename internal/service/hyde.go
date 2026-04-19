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
)

func GenerateHypotheticalDocument(ctx context.Context, ollamaURL, model, query string) (string, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": fmt.Sprintf("Given the following search query, write a short passage that would answer this query. Keep it under 200 words.\n\nQuery: %s", query),
			},
		},
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 60 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hyde ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("hyde ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("hyde ollama decode failed: %w", err)
	}

	content := strings.TrimSpace(result.Message.Content)
	return content, nil
}

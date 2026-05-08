package service

import (
	"os"
	"testing"
)

func TestLLMClientGenerate(t *testing.T) {
	modelPath := os.Getenv("LMD_TEST_SUMMARIZE_MODEL")
	if modelPath == "" {
		t.Skip("LMD_TEST_SUMMARIZE_MODEL not set, skipping LLM test")
	}

	client, err := NewLLMClient(modelPath, -1, 4)
	if err != nil {
		t.Fatalf("NewLLMClient failed: %v", err)
	}
	defer client.Close()

	prompt := "用一句话总结：Go 语言的主要特点是简洁、并发和高效。"
	text, err := client.Generate(prompt, 128)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty output")
	}
	t.Logf("LLM output: %s", text)
}

func TestLLMClientNotExist(t *testing.T) {
	_, err := NewLLMClient("/nonexistent/model.gguf", -1, 4)
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}

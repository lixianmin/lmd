package embedding

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLlamaProvider_Interface(t *testing.T) {
	var _ EmbeddingProvider = (*LlamaProvider)(nil)
}

func TestLlamaProvider_Dimension(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	if p.Dimension() != 1024 {
		t.Fatalf("expected 1024, got %d", p.Dimension())
	}
}

func TestLlamaProvider_ModelName(t *testing.T) {
	p := NewLlamaProvider("/some/path/model.gguf", -1, 4, 8)
	if p.ModelName() != "/some/path/model.gguf" {
		t.Fatalf("expected model path, got %s", p.ModelName())
	}
}

func TestLlamaProvider_Close_Idempotent(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	if err := p.Close(); err != nil {
		t.Fatalf("Close on unloaded provider should not error: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Second Close should not error: %v", err)
	}
}

func TestLlamaProvider_ModelNotFound(t *testing.T) {
	p := NewLlamaProvider("/nonexistent/model.gguf", -1, 4, 8)
	_, err := p.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for missing model file")
	}
}

func TestLlamaProvider_EmbedBatch_ModelNotFound(t *testing.T) {
	p := NewLlamaProvider("/nonexistent/model.gguf", -1, 4, 8)
	_, err := p.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error for missing model file")
	}
}

func TestLlamaProvider_EmbedQuery_ModelNotFound(t *testing.T) {
	p := NewLlamaProvider("/nonexistent/model.gguf", -1, 4, 8)
	_, err := p.EmbedQuery(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for missing model file")
	}
}

func TestLlamaProvider_ReleaseIfIdle_NotLoaded(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	released := p.ReleaseIfIdle(time.Minute)
	if released {
		t.Fatal("should not release when not loaded")
	}
}

func TestLlamaProvider_ReleaseIfIdle_NotYetIdle(t *testing.T) {
	p := NewLlamaProvider("/fake/model.gguf", -1, 4, 8)
	p.lastActive = time.Now()
	released := p.ReleaseIfIdle(10 * time.Minute)
	if released {
		t.Fatal("should not release when not idle")
	}
}

func TestEmbedQueryPrefix(t *testing.T) {
	mock := NewMockProvider(32)

	vecDirect, _ := mock.Embed(context.Background(), "hello")
	vecQuery, _ := mock.EmbedQuery(context.Background(), "hello")

	if fmt.Sprintf("%x", vecDirect) == fmt.Sprintf("%x", vecQuery) {
		t.Fatal("EmbedQuery should produce different embedding than Embed (due to instruction prefix)")
	}
}

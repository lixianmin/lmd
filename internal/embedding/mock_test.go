package embedding

import (
	"context"
	"math"
	"testing"
)

func TestMockProviderEmbed(t *testing.T) {
	p := NewMockProvider(4)
	vec, err := p.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(vec))
	}
}

func TestMockProviderBatch(t *testing.T) {
	p := NewMockProvider(4)
	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestMockProviderDimension(t *testing.T) {
	p := NewMockProvider(128)
	if p.Dimension() != 128 {
		t.Fatalf("expected dimension 128, got %d", p.Dimension())
	}
}

func TestMockProviderDeterministic(t *testing.T) {
	p := NewMockProvider(16)
	v1, _ := p.Embed(context.Background(), "test")
	v2, _ := p.Embed(context.Background(), "test")
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatal("same input should produce same vector")
		}
	}

	v3, _ := p.Embed(context.Background(), "different")
	allSame := true
	for i := range v1 {
		if v1[i] != v3[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Fatal("different inputs should produce different vectors")
	}
}

func TestMockProviderNormalized(t *testing.T) {
	p := NewMockProvider(32)
	vec, _ := p.Embed(context.Background(), "normalization test")
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if math.Abs(float64(norm)-1.0) > 0.01 {
		t.Fatalf("expected unit norm, got %f", norm)
	}
}

func TestMockProviderModelName(t *testing.T) {
	p := NewMockProvider(4)
	if p.ModelName() != "mock" {
		t.Fatalf("expected model name 'mock', got %s", p.ModelName())
	}
}

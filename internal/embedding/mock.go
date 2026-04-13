package embedding

import (
	"context"
	"crypto/sha256"
	"math"
)

type MockProvider struct {
	dim int
}

func NewMockProvider(dim int) *MockProvider {
	return &MockProvider{dim: dim}
}

func (m *MockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return m.textToVector(text), nil
}

func (m *MockProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = m.textToVector(t)
	}
	return vecs, nil
}

func (m *MockProvider) Dimension() int    { return m.dim }
func (m *MockProvider) ModelName() string { return "mock" }
func (m *MockProvider) Close() error      { return nil }

func (m *MockProvider) textToVector(text string) []float32 {
	vec := make([]float32, m.dim)
	h := sha256.Sum256([]byte(text))
	for i := range vec {
		b := h[i%len(h)]
		vec[i] = float32(b) / 256.0
	}
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

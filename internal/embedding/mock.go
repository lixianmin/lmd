package embedding

import (
	"context"
	"crypto/sha256"
	"math"
	"strings"
	"unicode"
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

func (m *MockProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return m.Embed(ctx, query)
}

func (m *MockProvider) Dimension() int    { return m.dim }
func (m *MockProvider) ModelName() string { return "mock" }
func (m *MockProvider) Close() error      { return nil }

func (m *MockProvider) textToVector(text string) []float32 {
	vec := make([]float32, m.dim)

	tokens := tokenizeForMock(text)
	if len(tokens) == 0 {
		return vec
	}

	slotSize := 8
	numSlots := m.dim / slotSize
	if numSlots < 1 {
		numSlots = 1
		slotSize = m.dim
	}

	for _, token := range tokens {
		h := sha256.Sum256([]byte(token))
		slot := int(h[0]) % numSlots
		base := slot * slotSize
		for j := 0; j < slotSize && base+j < m.dim; j++ {
			vec[base+j] += float32(h[j%32]) + 1.0
		}
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

func tokenizeForMock(text string) []string {
	var tokens []string
	var current []rune
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			if len(current) > 0 {
				word := strings.ToLower(string(current))
				for _, w := range strings.Fields(word) {
					tokens = append(tokens, w)
				}
				current = current[:0]
			}
			tokens = append(tokens, string(r))
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				tokens = append(tokens, strings.ToLower(string(current)))
				current = current[:0]
			}
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, strings.ToLower(string(current)))
	}
	return tokens
}

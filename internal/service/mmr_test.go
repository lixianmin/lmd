package service

import (
	"math"
	"testing"
)

func TestMMRBasic(t *testing.T) {
	query := []float32{1.0, 0.0}
	norm1 := float32(1.0)
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{norm1, 0.0}},
		{ID: 2, Embedding: []float32{0.998, 0.063}},
		{ID: 3, Embedding: []float32{0.0, norm1}},
	}

	selected := SelectMMR(candidates, query, 0.3, 2)

	if len(selected) != 2 {
		t.Fatalf("expected 2 results, got %d", len(selected))
	}
	if selected[0].ID != 1 {
		t.Fatalf("expected first selected to be most relevant (ID=1), got ID=%d", selected[0].ID)
	}
	if selected[1].ID != 3 {
		t.Fatalf("expected second selected to be diverse (ID=3), got ID=%d", selected[1].ID)
	}
}

func TestMMREmptyCandidates(t *testing.T) {
	query := []float32{1.0, 0.0}
	selected := SelectMMR(nil, query, 0.7, 5)
	if len(selected) != 0 {
		t.Fatalf("expected 0 results, got %d", len(selected))
	}
}

func TestMMRFewerThanTopK(t *testing.T) {
	query := []float32{1.0, 0.0}
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{0.9, 0.1}},
	}
	selected := SelectMMR(candidates, query, 0.7, 5)
	if len(selected) != 1 {
		t.Fatalf("expected 1 result, got %d", len(selected))
	}
}

func TestMMRAllIdentical(t *testing.T) {
	query := []float32{1.0, 0.0}
	emb := []float32{0.8, 0.2}
	var norm float64
	for _, v := range emb {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	normed := make([]float32, len(emb))
	for i, v := range emb {
		normed[i] = v / float32(norm)
	}

	candidates := []MMRCandidate{
		{ID: 1, Embedding: normed},
		{ID: 2, Embedding: normed},
		{ID: 3, Embedding: normed},
	}

	selected := SelectMMR(candidates, query, 0.7, 3)

	ids := make(map[int64]bool)
	for _, s := range selected {
		if ids[s.ID] {
			t.Fatalf("duplicate ID %d in selection", s.ID)
		}
		ids[s.ID] = true
	}
}

func TestMMRLambdaOne(t *testing.T) {
	query := []float32{1.0, 0.0}
	candidates := []MMRCandidate{
		{ID: 1, Embedding: []float32{0.5, 0.5}},
		{ID: 2, Embedding: []float32{0.9, 0.1}},
		{ID: 3, Embedding: []float32{0.1, 0.9}},
	}

	selected := SelectMMR(candidates, query, 1.0, 3)

	if selected[0].ID != 2 {
		t.Fatalf("lambda=1.0 should be pure relevance, expected ID=2 first, got ID=%d", selected[0].ID)
	}
}

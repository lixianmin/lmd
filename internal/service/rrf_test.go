package service

import (
	"math"
	"testing"

	"github.com/lixianmin/lmd/internal/formatter"
)

func TestRRFTwoListsEqualWeights(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{
			{ChunkId: 1, DocId: "a", Score: 0.8},
			{ChunkId: 2, DocId: "b", Score: 0.6},
		},
		{
			{ChunkId: 3, DocId: "c", Score: 0.9},
			{ChunkId: 1, DocId: "a", Score: 0.7},
		},
	}

	params := DefaultRRFParams()
	params.Weights = []float64{1.0, 1.0}
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	if result[0].ChunkId != 1 {
		t.Fatalf("expected chunk 1 first, got %d (score=%.6f)", result[0].ChunkId, result[0].Score)
	}
	if result[1].ChunkId != 3 {
		t.Fatalf("expected chunk 3 second, got %d (score=%.6f)", result[1].ChunkId, result[1].Score)
	}
	if result[2].ChunkId != 2 {
		t.Fatalf("expected chunk 2 third, got %d (score=%.6f)", result[2].ChunkId, result[2].Score)
	}
}

func TestRRFDefaultWeights(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{{ChunkId: 1, DocId: "a"}},
		{{ChunkId: 2, DocId: "b"}},
		{{ChunkId: 3, DocId: "c"}},
	}

	params := DefaultRRFParams()
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	if result[0].Score != 1.0 {
		t.Fatalf("rank 1: expected 1.0, got %.6f", result[0].Score)
	}
	if result[1].Score != 0.5 {
		t.Fatalf("rank 2: expected 0.5, got %.6f", result[1].Score)
	}
	if result[2].Score-0.333333 > 0.001 {
		t.Fatalf("rank 3: expected ~0.333, got %.6f", result[2].Score)
	}
}

func TestRRFTopRankBonus(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{
			{ChunkId: 1, DocId: "a"},
			{ChunkId: 2, DocId: "b"},
			{ChunkId: 3, DocId: "c"},
			{ChunkId: 4, DocId: "d"},
		},
	}

	params := DefaultRRFParams()
	params.Weights = []float64{1.0}
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	expected := []float64{1.0, 0.5, 1.0 / 3.0, 0.25}
	for i, exp := range expected {
		if math.Abs(result[i].Score-exp) > 1e-9 {
			t.Fatalf("rank %d: expected %.6f, got %.6f", i+1, exp, result[i].Score)
		}
	}
}

func TestRRFEmptyLists(t *testing.T) {
	params := DefaultRRFParams()

	result := ReciprocalRankFusion(nil, params)
	if len(result) != 0 {
		t.Fatalf("expected 0 for nil input, got %d", len(result))
	}

	result = ReciprocalRankFusion([][]formatter.SearchHit{}, params)
	if len(result) != 0 {
		t.Fatalf("expected 0 for empty input, got %d", len(result))
	}

	result = ReciprocalRankFusion([][]formatter.SearchHit{nil, nil}, params)
	if len(result) != 0 {
		t.Fatalf("expected 0 for nil lists, got %d", len(result))
	}
}

func TestRRFSingleList(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{
			{ChunkId: 1, DocId: "a", Score: 0.5},
			{ChunkId: 2, DocId: "b", Score: 0.3},
		},
	}

	params := DefaultRRFParams()
	params.Weights = []float64{1.0}
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ChunkId != 1 {
		t.Fatalf("expected chunk 1 first, got %d", result[0].ChunkId)
	}
	if result[1].ChunkId != 2 {
		t.Fatalf("expected chunk 2 second, got %d", result[1].ChunkId)
	}
}

func TestRRFSameChunkBothLists(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{{ChunkId: 1, DocId: "a", Score: 0.5}},
		{{ChunkId: 1, DocId: "a", Score: 0.8}},
	}

	params := DefaultRRFParams()
	params.Weights = []float64{1.0, 1.0}
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	if result[0].Score != 1.0 {
		t.Fatalf("expected score 1.0 for single result, got %.6f", result[0].Score)
	}
}

func TestRRFMultipleChunksSameDoc(t *testing.T) {
	lists := [][]formatter.SearchHit{
		{
			{ChunkId: 1, DocId: "a"},
			{ChunkId: 2, DocId: "a"},
		},
		{
			{ChunkId: 2, DocId: "a"},
			{ChunkId: 3, DocId: "a"},
		},
	}

	params := DefaultRRFParams()
	params.Weights = []float64{1.0, 1.0}
	result := ReciprocalRankFusion(lists, params)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	ids := map[int64]bool{}
	for _, h := range result {
		ids[h.ChunkId] = true
	}
	for _, id := range []int64{1, 2, 3} {
		if !ids[id] {
			t.Fatalf("expected chunk %d in results", id)
		}
	}
}

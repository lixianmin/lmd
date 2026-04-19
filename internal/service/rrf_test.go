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

	expected1 := 2.0/61.0 + 0.05
	expected3 := 1.0/61.0 + 0.05

	if math.Abs(result[0].Score-expected1) > 1e-9 {
		t.Fatalf("chunk1 (weight 2.0): expected %.6f, got %.6f", expected1, result[0].Score)
	}
	if math.Abs(result[2].Score-expected3) > 1e-9 {
		t.Fatalf("chunk3 (weight 1.0): expected %.6f, got %.6f", expected3, result[2].Score)
	}

	if result[2].Score >= result[0].Score {
		t.Fatalf("third list (weight 1.0) should score below first (weight 2.0): %.6f vs %.6f", result[2].Score, result[0].Score)
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

	score1 := 1.0/61.0 + 0.05
	score2 := 1.0/62.0 + 0.02
	score3 := 1.0/63.0 + 0.02
	score4 := 1.0 / 64.0

	if math.Abs(result[0].Score-score1) > 1e-9 {
		t.Fatalf("rank0 bonus: expected %.6f, got %.6f", score1, result[0].Score)
	}
	if math.Abs(result[1].Score-score2) > 1e-9 {
		t.Fatalf("rank1 bonus: expected %.6f, got %.6f", score2, result[1].Score)
	}
	if math.Abs(result[2].Score-score3) > 1e-9 {
		t.Fatalf("rank2 bonus: expected %.6f, got %.6f", score3, result[2].Score)
	}
	if math.Abs(result[3].Score-score4) > 1e-9 {
		t.Fatalf("rank3 no bonus: expected %.6f, got %.6f", score4, result[3].Score)
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

	expected := 1.0/61.0 + 1.0/61.0 + 0.05
	if math.Abs(result[0].Score-expected) > 1e-9 {
		t.Fatalf("expected combined score %.6f, got %.6f", expected, result[0].Score)
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

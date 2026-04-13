package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/formatter"
)

func TestRRFFusionBasic(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
		{DocID: "c", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{DocID: "c", Score: 1.0},
		{DocID: "a", Score: 0.9},
		{DocID: "d", Score: 0.6},
	}

	result := FuseRRF(lexHits, vecHits, 60, 1.0)

	if len(result) == 0 {
		t.Fatal("expected fused results")
	}

	firstIDs := make(map[string]bool)
	for _, h := range result[:2] {
		firstIDs[h.DocID] = true
	}
	if !firstIDs["a"] || !firstIDs["c"] {
		t.Fatalf("expected 'a' and 'c' to rank highest, got top 2: %s and %s", result[0].DocID, result[1].DocID)
	}
}

func TestRRFFusionEmptyLex(t *testing.T) {
	vecHits := []formatter.SearchHit{
		{DocID: "a", Score: 1.0},
	}
	result := FuseRRF(nil, vecHits, 60, 1.0)
	if len(result) != 1 || result[0].DocID != "a" {
		t.Fatal("expected vector-only results when lex is empty")
	}
}

func TestRRFFusionEmptyVec(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocID: "b", Score: 1.0},
	}
	result := FuseRRF(lexHits, nil, 60, 1.0)
	if len(result) != 1 || result[0].DocID != "b" {
		t.Fatal("expected lex-only results when vec is empty")
	}
}

func TestRRFFusionBothEmpty(t *testing.T) {
	result := FuseRRF(nil, nil, 60, 1.0)
	if len(result) != 0 {
		t.Fatal("expected empty result for empty inputs")
	}
}

func TestRRFDeduplication(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
	}
	result := FuseRRF(lexHits, vecHits, 60, 1.0)
	seen := map[string]int{}
	for _, h := range result {
		seen[h.DocID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("doc %s appeared %d times (should be deduplicated)", id, count)
		}
	}
}

func TestRRFScoreOrdering(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocID: "a", Score: 1.0},
		{DocID: "b", Score: 0.8},
		{DocID: "e", Score: 0.3},
	}
	vecHits := []formatter.SearchHit{
		{DocID: "c", Score: 1.0},
		{DocID: "a", Score: 0.9},
		{DocID: "d", Score: 0.7},
	}
	result := FuseRRF(lexHits, vecHits, 60, 1.0)
	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Fatalf("results not sorted by score: [%d]=%.4f > [%d]=%.4f",
				i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}

package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/formatter"
)

func TestFuseResultsBasic(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.8},
		{ChunkId: 2, DocId: "b", Score: 0.6},
		{ChunkId: 3, DocId: "c", Score: 0.3},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 3, DocId: "c", Score: 0.9},
		{ChunkId: 1, DocId: "a", Score: 0.7},
		{ChunkId: 4, DocId: "d", Score: 0.5},
	}

	result := FuseResults(lexHits, vecHits, 0.7)

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	if result[0].ChunkId != 1 {
		t.Fatalf("expected chunk 1 to rank highest, got chunk %d (score=%.4f)", result[0].ChunkId, result[0].Score)
	}
}

func TestFuseResultsScoreCalculation(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 1.0},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 1.0},
	}

	result := FuseResults(lexHits, vecHits, 0.7)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}

	expected := 0.3*1.0 + 0.7*1.0
	if result[0].Score != expected {
		t.Fatalf("expected score %.2f, got %.4f", expected, result[0].Score)
	}
}

func TestFuseResultsVectorOnly(t *testing.T) {
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.8},
	}
	result := FuseResults(nil, vecHits, 0.7)
	if len(result) != 1 || result[0].ChunkId != 1 {
		t.Fatal("expected vector-only results when lex is empty")
	}
	expected := 0.7 * 0.8
	if diff := result[0].Score - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected score %.4f, got %.4f", expected, result[0].Score)
	}
}

func TestFuseResultsLexOnly(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 2, DocId: "b", Score: 0.9},
	}
	result := FuseResults(lexHits, nil, 0.7)
	if len(result) != 1 || result[0].ChunkId != 2 {
		t.Fatal("expected lex-only results when vec is empty")
	}
	expected := 0.3 * 0.9
	if diff := result[0].Score - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected score %.4f, got %.4f", expected, result[0].Score)
	}
}

func TestFuseResultsBothEmpty(t *testing.T) {
	result := FuseResults(nil, nil, 0.7)
	if len(result) != 0 {
		t.Fatal("expected empty result for empty inputs")
	}
}

func TestFuseResultsChunkDeduplication(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 1.0},
		{ChunkId: 2, DocId: "a", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 1.0},
		{ChunkId: 3, DocId: "a", Score: 0.8},
	}
	result := FuseResults(lexHits, vecHits, 0.7)

	seen := map[int64]int{}
	for _, h := range result {
		seen[h.ChunkId]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("chunk %d appeared %d times (should be deduplicated)", id, count)
		}
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 unique chunks (1,2,3), got %d", len(result))
	}
}

func TestFuseResultsOrdering(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.9},
		{ChunkId: 2, DocId: "b", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 2, DocId: "b", Score: 0.95},
		{ChunkId: 1, DocId: "a", Score: 0.3},
	}
	result := FuseResults(lexHits, vecHits, 0.7)

	scoreA := 0.3*0.9 + 0.7*0.3
	scoreB := 0.3*0.5 + 0.7*0.95

	if scoreB <= scoreA {
		t.Fatalf("test setup error: b (%.4f) should score higher than a (%.4f)", scoreB, scoreA)
	}

	if result[0].ChunkId != 2 {
		t.Fatalf("expected chunk 2 first (score=%.4f > chunk 1=%.4f), got %d", scoreB, scoreA, result[0].ChunkId)
	}

	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Fatalf("results not sorted: [%d]=%.4f > [%d]=%.4f", i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}

func TestFuseResultsSnippetMerge(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.8, Snippet: "lex snippet"},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.9, Snippet: "vec snippet"},
	}
	result := FuseResults(lexHits, vecHits, 0.7)
	if len(result) != 1 {
		t.Fatal("expected 1 result")
	}
	if result[0].Snippet != "lex snippet" {
		t.Fatalf("expected lex snippet to be preserved, got %q", result[0].Snippet)
	}
}

func TestFuseResultsSnippetFillFromVec(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.8, Snippet: ""},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.9, Snippet: "vec snippet"},
	}
	result := FuseResults(lexHits, vecHits, 0.7)
	if len(result) != 1 {
		t.Fatal("expected 1 result")
	}
	if result[0].Snippet != "vec snippet" {
		t.Fatalf("expected vec snippet to fill empty lex snippet, got %q", result[0].Snippet)
	}
}

func TestFuseResultsNoTopScoreNormalization(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.3},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.4},
	}
	result := FuseResults(lexHits, vecHits, 0.7)

	expected := 0.3*0.3 + 0.7*0.4
	if result[0].Score != expected {
		t.Fatalf("expected raw combined score %.4f, got %.4f (should NOT normalize to 1.0)", expected, result[0].Score)
	}
	if result[0].Score == 1.0 {
		t.Fatal("score should NOT be normalized to 1.0")
	}
}

func TestFuseResultsMultipleChunksSameDoc(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{ChunkId: 1, DocId: "a", Score: 0.8},
		{ChunkId: 2, DocId: "a", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{ChunkId: 2, DocId: "a", Score: 0.9},
		{ChunkId: 3, DocId: "a", Score: 0.4},
	}
	result := FuseResults(lexHits, vecHits, 0.7)

	if len(result) != 3 {
		t.Fatalf("expected 3 unique chunks, got %d", len(result))
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

	score1 := 0.3 * 0.8
	score2 := 0.3*0.5 + 0.7*0.9
	score3 := 0.7 * 0.4

	if result[0].ChunkId != 2 {
		t.Fatalf("expected chunk 2 first (score=%.4f), got chunk %d (score=%.4f)", score2, result[0].ChunkId, result[0].Score)
	}
	_ = score1
	_ = score3
}

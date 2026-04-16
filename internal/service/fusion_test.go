package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/formatter"
)

func TestFuseResultsBasic(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.8},
		{DocId: "b", Score: 0.6},
		{DocId: "c", Score: 0.3},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "c", Score: 0.9},
		{DocId: "a", Score: 0.7},
		{DocId: "d", Score: 0.5},
	}

	result := FuseResults(lexHits, vecHits, 0.7)

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	if result[0].DocId != "a" {
		t.Fatalf("expected 'a' to rank highest, got %s (score=%.4f)", result[0].DocId, result[0].Score)
	}
}

func TestFuseResultsScoreCalculation(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "a", Score: 1.0},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "a", Score: 1.0},
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
		{DocId: "a", Score: 0.8},
	}
	result := FuseResults(nil, vecHits, 0.7)
	if len(result) != 1 || result[0].DocId != "a" {
		t.Fatal("expected vector-only results when lex is empty")
	}
	expected := 0.7 * 0.8
	if diff := result[0].Score - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected score %.4f, got %.4f", expected, result[0].Score)
	}
}

func TestFuseResultsLexOnly(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "b", Score: 0.9},
	}
	result := FuseResults(lexHits, nil, 0.7)
	if len(result) != 1 || result[0].DocId != "b" {
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

func TestFuseResultsDeduplication(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "a", Score: 1.0},
		{DocId: "b", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "a", Score: 1.0},
		{DocId: "b", Score: 0.8},
	}
	result := FuseResults(lexHits, vecHits, 0.7)
	seen := map[string]int{}
	for _, h := range result {
		seen[h.DocId]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("doc %s appeared %d times (should be deduplicated)", id, count)
		}
	}
}

func TestFuseResultsOrdering(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.9},
		{DocId: "b", Score: 0.5},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "b", Score: 0.95},
		{DocId: "a", Score: 0.3},
	}
	result := FuseResults(lexHits, vecHits, 0.7)

	scoreA := 0.3*0.9 + 0.7*0.3
	scoreB := 0.3*0.5 + 0.7*0.95

	if scoreB <= scoreA {
		t.Fatalf("test setup error: b (%.4f) should score higher than a (%.4f)", scoreB, scoreA)
	}

	if result[0].DocId != "b" {
		t.Fatalf("expected 'b' first (score=%.4f > a=%.4f), got %s", scoreB, scoreA, result[0].DocId)
	}

	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Fatalf("results not sorted: [%d]=%.4f > [%d]=%.4f", i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}

func TestFuseResultsSnippetMerge(t *testing.T) {
	lexHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.8, Snippet: "lex snippet"},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.9, Snippet: "vec snippet"},
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
		{DocId: "a", Score: 0.8, Snippet: ""},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.9, Snippet: "vec snippet"},
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
		{DocId: "a", Score: 0.3},
	}
	vecHits := []formatter.SearchHit{
		{DocId: "a", Score: 0.4},
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

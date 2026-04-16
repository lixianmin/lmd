# Search Fusion Improvement

## Problem

RRF (Reciprocal Rank Fusion) discards raw score magnitudes — only rank positions matter. Combined with normalization to [0,1] (dividing by top score), this produces:
1. Top result always scores 1.0 (meaningless)
2. Semantically different results get near-identical scores
3. Irrelevant results (curl.md) can outrank relevant ones (shell.md)

## Solution

Replace RRF with **weighted linear combination**, matching OpenClaw and Hermes-Agent:

1. BM25 score → normalize to [0,1] via `abs(rank)/max_rank` (Hermes approach)
2. Vector score → already [0,1] via `1 - cosine_distance` (unchanged)
3. Combine: `0.7 * vectorScore + 0.3 * bm25Score`
4. Remove top-score normalization

## Changes

### 1. `internal/service/fusion.go` — Replace RRF with weighted combination

```go
// Before: RRF using rank positions only
func ReciprocalRankFusion(lexHits, vecHits []formatter.SearchHit, k int, origWeight float64) []formatter.SearchHit

// After: Weighted linear combination
func FuseResults(lexHits, vecHits []formatter.SearchHit, vectorWeight float64) []formatter.SearchHit
```

Logic:
- Group hits by DocId
- Each doc gets `vectorWeight * vectorScore + (1-vectorWeight) * bm25Score`
- If a doc only appears in one result set, the other score is 0
- Sort by combined score descending
- No normalization — raw combined score as output

### 2. `internal/service/searcher.go:114` — Update call site

```go
// Before
fused := ReciprocalRankFusion(lexHits, vecHits, 60, 1.0)

// After
fused := FuseResults(lexHits, vecHits, 0.7)
```

### 3. `internal/service/fusion_test.go` — Update tests

Rewrite tests for new `FuseResults` function.

## Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| vectorWeight | 0.7 | Vector search captures semantic meaning better than keyword matching |
| textWeight | 0.3 (= 1 - vectorWeight) | BM25 provides lexical precision as complement |

## Scope

- Only `fusion.go`, `fusion_test.go`, and `searcher.go` need changes
- `SearchLex` and `SearchVector` unchanged — their scores are already in [0,1]
- `lmd query` is the only command using hybrid fusion

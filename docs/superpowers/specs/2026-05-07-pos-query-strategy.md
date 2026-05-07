# POS-based Query Strategy

## Motivation

GSE tokenizer's default POS tagger marks all English words as "x" (unknown). Without POS
information, the FTS5 OR query includes stop words and low-value terms (pronouns, prepositions,
articles), degrading precision. Chinese queries benefit from GSE's built-in POS tagging but
English queries need a supplementary word→POS map.

## Approach

Two-step POS-based query rewriting:

1. **POS tagging**: GSE tokenizer for Chinese (n, v, a, etc.) + Brown Corpus lookup for English
2. **Term filtering**: Keep only content words (nouns + verbs + adjectives), drop everything else

This removes ~30-40% of query terms (stop words, pronouns, prepositions) while retaining
semantically meaningful words.

## Three Strategies

### pos-or (default, recommended)

Filter to nouns + verbs + adjectives, join with OR. Same semantics as "or" strategy but with
cleaner term sets. Best balance of recall and FTS5 query efficiency.

### pos-must (experimental, not recommended)

Nouns AND-connected, verbs/adjectives OR-connected within a SHOULD group:
`noun1 AND noun2 AND (verb1 OR verb2)`. Too strict for 300-rune chunks — uncommon for a single
chunk to contain all query nouns.

### pos-weight (experimental, not recommended)

Standard OR query + post-retrieval score boost based on noun/verb overlap in snippet. Boost
magnitude (0.02-0.05) is too small relative to BM25 score variance to reliably reorder results.

## English POS Data Source

- **Source**: Brown Corpus via NLTK (https://www.nltk.org/book/ch02.html)
- **File**: `internal/tokenizer/en_pos.go`
- **Size**: 46,066 entries
- **Distribution**: n=28,240 (61.3%), v=8,814 (19.1%), adj=7,349 (16.0%), adv=1,663 (3.6%)
- **Lookup**: `gse.go` checks `enPosMap` when GSE returns tag "x" for English tokens

## Benchmark (LongMemEval, 150 queries, 970K chunks)

| Strategy | R@5 | Speed | Notes |
|----------|-----|-------|-------|
| or | 33.3% | 12.8 q/s | Baseline OR, all terms |
| pos-or | 33.3% | 12.8 q/s | Fewer terms, same recall |
| pos-must | ~28% | — | Too strict for 300-rune chunks |
| pos-weight | ~33% | — | Reordering noise ~= null effect |
| df | 31.5% | 13.1 q/s | Top-5 rarest terms |
| and | 0% | 5.0 q/s | Requires all terms co-occur |

pos-or matches "or" recall while using fewer (higher quality) terms — effectively reducing
noise without losing signal.

## `--strategy` flag values

| Value | Behavior |
|-------|----------|
| `pos-or` (default) | POS filter → OR |
| `or` | Tokenize → OR, no POS filter |
| `df` | Top-5 rarest terms by IDF/count → OR |
| `pos-must` | Nouns AND + verbs OR |
| `pos-weight` | OR + post-retrieval noun/verb boost |
| `and` | QMD-style AND + bm25() |

## Design Decisions

1. Default changed from "or" → "pos-or" (2026-05-07): cleaner queries, same recall
2. pos-must retained in code but documented as experimental: AND semantics incompatible with
   300-rune chunks
3. pos-weight retained in code but documented as ineffective: boost magnitude too small
4. Brown Corpus map pre-computed at code-gen time, zero runtime cost for English POS lookup

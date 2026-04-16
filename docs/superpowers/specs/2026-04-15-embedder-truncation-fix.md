# Embedder Truncation Fix

## Problem

Vector search returns identical scores (0.6709) for all results. Two root causes:

1. `embedder.go:63` uses byte-level truncation `t[:512]` — splits UTF-8 multi-byte characters (Chinese = 3 bytes), producing garbled input to the embedding model
2. Many chunks from the old chunker share identical prefixes (e.g., `\`\`\`shell\ncommand...`), so 512-byte truncation produces identical text → identical embeddings → identical scores

## Fix: Rune-level Truncation

**File:** `internal/service/embedder.go`

Change document truncation from byte-level 512 to rune-level 800:

```go
// Before
t := c.Content
if len(t) > 512 {
    t = t[:512]
}
texts[i] = t

// After
t := c.Content
runes := []rune(t)
if len(runes) > 800 {
    t = string(runes[:800])
}
texts[i] = t
```

Rationale: New chunker produces chunks ≤ 1000 runes (hardMax). 800 runes captures most semantic content without exceeding the truncation. Rune-level truncation never splits a UTF-8 character. Query path is unaffected since truncation only occurs here for document chunks.

## Verification

After fix, run `lmd rebuild` to re-chunk and re-embed all documents. Existing data with garbled embeddings will persist until rebuild. Expected: scores differentiate across results, snippets show meaningful content.

## Scope

- Only `internal/service/embedder.go:60-66` needs code changes
- Follow-up: `lmd rebuild` required to regenerate all data

# Chunker v3: Sliding Window with Scored Breakpoints

## Why Rewrite Again

Recursive splitting (v2) has a fundamental flaw: it splits first then tries to merge back. This produces tiny fragments like `----\n\ncurl` that carry no semantic meaning and pollute embedding quality. The merge-small patch is a band-aid.

QMD uses a sliding window approach that never produces fragments: walk left-to-right, at each target position find the best breakpoint within a search window using scored patterns + distance decay.

## Algorithm

All positions and sizes are in **runes**, not bytes.

### Step 1: Preprocessing

- Strip base64 inline images → `...(truncated)`
- Prepend `\n` to ensure heading patterns match at position 0 (strip the extra `\n` from output)

### Step 2: Scan Breakpoints

Scan the document once, recording positions with scores:

| Pattern | Score | Meaning |
|---------|-------|---------|
| `\n# ` (not `##`) | 100 | h1 heading |
| `\n## ` (not `###`) | 90 | h2 heading |
| `\n### ` (not `####`) | 80 | h3 heading |
| `\n#### ` | 70 | h4 heading |
| `` \n``` `` | 80 | Code fence boundary |
| `\n---\n` / `\n***\n` | 60 | Horizontal rule |
| `\n\n` | 20 | Paragraph boundary |

Also detect code fence regions (between `` ``` `` pairs). Breakpoints inside code fence regions are excluded from cutting.

### Step 3: Sliding Window Cut

```
charPos = 0
while charPos < runeCount(content):
    targetEnd = min(charPos + chunkSize, runeCount(content))
    if targetEnd == runeCount(content):
        emit chunk [charPos, targetEnd]
        break

    // Search window: [targetEnd - windowChars, targetEnd]
    bestBreak = targetEnd
    bestScore = 0
    for each breakpoint bp in window:
        if bp inside code fence: skip
        distance = targetEnd - bp.pos
        normalizedDist = distance / windowChars
        multiplier = 1.0 - (normalizedDist² × 0.7)
        finalScore = bp.score × multiplier
        if finalScore > bestScore:
            bestScore = finalScore
            bestBreak = bp.pos

    emit chunk [charPos, bestBreak]
    charPos = bestBreak  // NO overlap rewind here; overlap handled in Step 4
```

### Step 4: Hard Split

After cutting, if any chunk exceeds `hardMax` (1000 runes), split it by rune at `hardMax` intervals. This handles pathological input (e.g., a 2000-char line with no breakpoints).

### Step 5: Merge Small

Iterate chunks in order. If a chunk < `minChunkSize` (100 runes), merge it into the previous chunk if the combined size ≤ `hardMax`. If it's the first chunk, merge into the next chunk instead.

### Step 6: Overlap

Post-processing only (no charPos rewind). For adjacent chunks A and B:
- Take the last ~100 runes of chunk A
- Find the nearest sentence boundary (。！？.!?) within ±50 runes of that point
- Prepend that text to chunk B
- Skip if overlap would push chunk B above `hardMax`

### Step 7: Metadata

Each chunk records:
- `StartLine`: first line number (0-indexed) in the original document
- `EndLine`: last line number (0-indexed, inclusive) in the original document
- `TokenCount`: `ascii/4 + cjk*2`

These two fields allow exact location in the original file: lines `[StartLine, EndLine]`.

The `title` parameter is removed from the interface — it was unused.

## Parameters

All derived from `chunkSize` in constructor:

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| chunkSize | 800 runes | Target size, ~200-300 CJK chars |
| hardMax | chunkSize + 200 = 1000 | Absolute ceiling |
| overlapChars | 100 runes | ~12% overlap |
| windowChars | 200 runes | Search window for best breakpoint |
| decayFactor | 0.7 | Distance decay strength |
| minChunkSize | 100 runes | Chunks below this are merged into neighbors |

Constructor: `NewMarkdownChunker(chunkSize int)` — if chunkSize ≤ 0, default to 800.

## Implementation

**File:** `internal/chunker/markdown.go` — full rewrite
**File:** `internal/chunker/chunker.go` — `Chunk` struct: `Position int` → `StartLine int` + `EndLine int`; `Chunker` interface: remove `title` param
**File:** `internal/service/indexer.go` — update `createChunks`: map `StartLine` → `Position` (DB field unchanged)
**DB:** No schema change — `position` column still stores the start line number
**Tests:** `internal/chunker/markdown_test.go` — full rewrite

## Why This Is Better

1. **No fragments**: Window always finds the best cut point. Tiny chunks get merged (minChunkSize=100).
2. **Code-fence aware**: Won't cut inside code blocks (breakpoints inside fences are excluded).
3. **Distance decay**: A heading (score 100) far from target still beats a newline (score 1) nearby.
4. **Single pass**: One scan for breakpoints, one pass for cutting. No recursive calls.
5. **Proven**: Same algorithm as QMD, battle-tested in production.
6. **Hard split safety net**: Pathological long lines still get handled (hardMax enforcement).

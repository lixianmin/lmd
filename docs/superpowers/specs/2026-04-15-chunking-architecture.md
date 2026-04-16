# Chunking Architecture

## Design Goals

- Every chunk ≤ 1000 characters (hard limit)
- Target chunk size: ~800 characters (~200-300 CJK chars, ~400 tokens)
- Overlap between adjacent chunks: ~100 characters, snapped to sentence boundary
- No chunk should break mid-sentence in the overlap region

## Algorithm

### Step 1: Preprocessing

- Strip base64-encoded inline images (`![...](data:image/...;base64,...)`)
- Replace with placeholder `...(truncated)`

### Step 2: Recursive Splitting

Split the document using a priority hierarchy of separators:

| Priority | Separator | Meaning |
|---|---|---|
| 1 | `\n\n` | Paragraph boundary |
| 2 | `\n` | Line boundary |
| 3 | `。！？.!?` | Sentence boundary (CJK + ASCII) |
| 4 | `；，；,` | Clause boundary |
| 5 | (single character) | Hard cut (last resort) |

Process:
1. Split text by highest-priority separator
2. Merge segments up to `chunkSize=800` chars
3. If a single segment > `hardMax=1000` chars, re-split it with next-priority separator
4. Recurse until all chunks ≤ hardMax

### Step 3: Overlap

When splitting produces adjacent chunks A and B:
- Take the last ~100 characters of chunk A
- Find the nearest sentence boundary (。！？.!?) within ±50 chars of that point
- Prepend that sentence to chunk B as overlap
- This ensures no sentence is cut in half

### Step 4: Metadata

Each chunk records:
- `Position`: line number of the first line in the original document
- `TokenCount`: estimated tokens (`ascii/4 + cjk*2`)

## Parameters

| Parameter | Value | Rationale |
|---|---|---|
| chunkSize | 800 chars | ~400 tokens, fits embedding model context efficiently |
| hardMax | 1000 chars | Absolute ceiling, no chunk exceeds this |
| overlap | ~100 chars (~12%) | Maintains cross-chunk context, snapped to sentence boundary |

## Implementation

File: `internal/chunker/markdown.go`
Interface: `Chunker.Chunk(title, body string) ([]Chunk, error)`
Caller: `internal/service/indexer.go` creates `NewMarkdownChunker(800)`

## Why This Design

1. **Recursive over heading-based**: Headings are unreliable — some sections are 5KB, others 2 lines. Recursive splitting produces uniform chunk sizes.
2. **Sentence-aware overlap**: Overlap snaps to sentence boundaries, so embedding models always see complete sentences.
3. **Small chunks = fast embedding**: 800 chars → 512 char truncation in embedder → ~127ms/chunk on M4 GPU.
4. **CJK-aware token estimation**: `ascii/4 + cjk*2` approximates real tokenizer behavior for mixed Chinese/English text.

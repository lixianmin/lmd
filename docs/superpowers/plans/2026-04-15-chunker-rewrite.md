# Chunker Rewrite Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the heading-based chunker with a recursive text splitter that produces uniformly-sized chunks with sentence-aware overlap.

**Architecture:** Preprocess (strip base64 images) → Recursive split by separator hierarchy (paragraph → line → sentence → clause → char) with merge-up-to-chunkSize and hardMax enforcement → Add sentence-boundary overlap between adjacent chunks. The algorithm is purely string-based with no markdown AST dependency.

**Tech Stack:** Go standard library only (`strings`, `regexp`, `unicode/utf8`). No new dependencies.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/chunker/chunker.go` | `Chunk` struct and `Chunker` interface (unchanged) |
| `internal/chunker/markdown.go` | `MarkdownChunker` — full rewrite with recursive splitting + overlap |
| `internal/chunker/markdown_test.go` | Full rewrite of tests for new algorithm |
| `internal/service/indexer.go:33` | Change `NewMarkdownChunker(900)` → `NewMarkdownChunker(800)` |

---

## Chunk 1: Tests + Implementation

### Task 1: Write failing tests for the new chunker

**Files:**
- Rewrite: `internal/chunker/markdown_test.go`

- [ ] **Step 1: Write all failing tests**

```go
package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkEmptyBody(t *testing.T) {
	chunks, err := NewMarkdownChunker(800).Chunk("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != text {
		t.Fatalf("expected %q, got %q", text, chunks[0].Content)
	}
	if chunks[0].Position != 0 {
		t.Fatalf("expected position 0, got %d", chunks[0].Position)
	}
}

func TestChunkPositionIsLineNumber(t *testing.T) {
	text := "line0\n\nline2\n\nline4"
	chunks, err := NewMarkdownChunker(5).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	if chunks[0].Position != 0 {
		t.Fatalf("expected first chunk position=0, got %d", chunks[0].Position)
	}
	if len(chunks) >= 2 && chunks[1].Position <= chunks[0].Position {
		t.Fatalf("expected second chunk position > first, got %d <= %d", chunks[1].Position, chunks[0].Position)
	}
}

func TestChunkRespectsHardMax(t *testing.T) {
	text := strings.Repeat("a", 2000)
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for 2000-char text, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkRecursiveSplitByParagraph(t *testing.T) {
	paraA := strings.Repeat("word ", 200)
	paraB := strings.Repeat("other ", 200)
	text := paraA + "\n\n" + paraB
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkSentenceBoundarySplit(t *testing.T) {
	longText := strings.Repeat("这是一个很长的句子。", 100)
	chunks, err := NewMarkdownChunker(800).Chunk("", longText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkClauseBoundarySplit(t *testing.T) {
	longText := strings.Repeat("这是很长的子句，", 100)
	chunks, err := NewMarkdownChunker(800).Chunk("", longText)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for clause-split text, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkOverlap(t *testing.T) {
	para := strings.Repeat("This is a test sentence. ", 80)
	text := para + "\n\n" + para
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for overlap test, got %d", len(chunks))
	}
	if chunks[0].Content == "" || chunks[1].Content == "" {
		t.Fatal("chunks should not be empty")
	}
}

func TestChunkOverlapDoesNotExceedHardMax(t *testing.T) {
	para := strings.Repeat("This is a test sentence. ", 80)
	text := para + "\n\n" + para
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d with overlap exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkStripsBase64Images(t *testing.T) {
	text := "# Title\n\nSome text here.\n\n![img](data:image/png;base64," + strings.Repeat("A", 5000) + ")\n\nMore text."
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "AAAA") {
			t.Fatal("base64 data should be stripped from chunks")
		}
	}
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "...(truncated)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ...(truncated) placeholder in at least one chunk")
	}
}

func TestChunkTokenEstimation(t *testing.T) {
	text := "Hello world"
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	if chunks[0].TokenCount <= 0 {
		t.Fatalf("expected positive token count, got %d", chunks[0].TokenCount)
	}
}

func TestChunkCJKTokenEstimation(t *testing.T) {
	text := "你好世界"
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	if chunks[0].TokenCount != 8 {
		t.Fatalf("expected 8 tokens for 4 CJK chars, got %d", chunks[0].TokenCount)
	}
}

func TestChunkLargeCodeBlock(t *testing.T) {
	code := strings.Repeat("fmt.Println(\"hello\")\n", 100)
	text := "```go\n" + code + "```\n"
	chunks, err := NewMarkdownChunker(800).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 1000 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestNewMarkdownChunkerDefault(t *testing.T) {
	c := NewMarkdownChunker(0)
	if c.chunkSize != 800 {
		t.Fatalf("expected default chunkSize=800, got %d", c.chunkSize)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/chunker/ -v -run 'TestChunk|TestNew'`
Expected: Multiple failures (old algorithm doesn't produce same results, new struct fields missing)

---

### Task 2: Implement the recursive chunker

**Files:**
- Rewrite: `internal/chunker/markdown.go`

- [ ] **Step 3: Implement the new MarkdownChunker**

```go
package chunker

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type MarkdownChunker struct {
	chunkSize int
	hardMax   int
	overlap   int
}

func NewMarkdownChunker(chunkSize int) *MarkdownChunker {
	if chunkSize <= 0 {
		chunkSize = 800
	}
	return &MarkdownChunker{
		chunkSize: chunkSize,
		hardMax:   chunkSize + 200,
		overlap:   100,
	}
}

var base64ImgRe = regexp.MustCompile(`!\[[^\]]*\]\(data:image/[^;]+;base64,[^)]*\)`)

var separatorRes []*regexp.Regexp

func init() {
	separatorRes = make([]*regexp.Regexp, len(separators))
	for i, s := range separators {
		if s.join == "" {
			separatorRes[i] = regexp.MustCompile(s.pattern)
		}
	}
}

func (my *MarkdownChunker) Chunk(title string, body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	body = my.stripBase64Images(body)
	segments := my.recursiveSplit(body, 0)
	merged := my.mergeSegments(segments, my.chunkSize)
	merged = my.enforceHardMax(merged)
	chunks := my.buildChunks(merged)
	chunks = my.addOverlap(chunks)
	return chunks, nil
}

func (my *MarkdownChunker) stripBase64Images(body string) string {
	return base64ImgRe.ReplaceAllString(body, "...(truncated)")
}

type separator struct {
	pattern string
	join    string
}

var separators = []separator{
	{`\n\n+`, "\n\n"},
	{`\n`, "\n"},
	{`[。！？.!?]`, ""},
	{`[；，;,]`, ""},
}

func splitPreserve(text string, level int) []string {
	sep := separators[level]
	if sep.join != "" {
		return strings.Split(text, sep.join)
	}
	re := separatorRes[level]
	locs := re.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return []string{text}
	}
	var parts []string
	prev := 0
	for _, loc := range locs {
		end := loc[1]
		parts = append(parts, text[prev:end])
		prev = end
	}
	if prev < len(text) {
		parts = append(parts, text[prev:])
	}
	return parts
}

func (my *MarkdownChunker) recursiveSplit(text string, level int) []string {
	if utf8.RuneCountInString(text) <= my.hardMax {
		return []string{text}
	}

	if level >= len(separators) {
		return my.splitByChar(text)
	}

	sep := separators[level]
	parts := splitPreserve(text, level)

	var segments []string
	var current strings.Builder

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if current.Len() > 0 {
			merged := current.String() + sep.join + part
			if utf8.RuneCountInString(merged) <= my.chunkSize {
				current.WriteString(sep.join)
				current.WriteString(part)
				continue
			}

			segments = append(segments, current.String())
			current.Reset()
		}

		if utf8.RuneCountInString(part) <= my.hardMax {
			current.WriteString(part)
		} else {
			sub := my.recursiveSplit(part, level+1)
			segments = append(segments, sub...)
		}
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}

func (my *MarkdownChunker) splitByChar(text string) []string {
	runes := []rune(text)
	var result []string
	for i := 0; i < len(runes); i += my.hardMax {
		end := i + my.hardMax
		if end > len(runes) {
			end = len(runes)
		}
		result = append(result, string(runes[i:end]))
	}
	return result
}

func (my *MarkdownChunker) mergeSegments(segments []string, maxSize int) []string {
	if len(segments) == 0 {
		return nil
	}

	var merged []string
	var current strings.Builder

	for _, seg := range segments {
		segLen := utf8.RuneCountInString(seg)
		curLen := utf8.RuneCountInString(current.String())

		if current.Len() > 0 && curLen+2+segLen > maxSize {
			merged = append(merged, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(seg)
	}

	if current.Len() > 0 {
		merged = append(merged, current.String())
	}

	return merged
}

func (my *MarkdownChunker) enforceHardMax(segments []string) []string {
	var result []string
	for _, seg := range segments {
		if utf8.RuneCountInString(seg) <= my.hardMax {
			result = append(result, seg)
		} else {
			runes := []rune(seg)
			for i := 0; i < len(runes); i += my.hardMax {
				end := i + my.hardMax
				if end > len(runes) {
					end = len(runes)
				}
				result = append(result, string(runes[i:end]))
			}
		}
	}
	return result
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}

func (my *MarkdownChunker) buildChunks(segments []string) []Chunk {
	chunks := make([]Chunk, 0, len(segments))
	pos := 0
	for _, seg := range segments {
		chunks = append(chunks, Chunk{
			Content:    strings.TrimSpace(seg),
			Position:   pos,
			TokenCount: estimateTokens(seg),
		})
		pos += lineCount(seg)
	}
	return chunks
}

func (my *MarkdownChunker) addOverlap(chunks []Chunk) []Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	result := make([]Chunk, len(chunks))
	copy(result, chunks)

	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1].Content
		runes := []rune(prev)

		if len(runes) <= my.overlap {
			continue
		}

		windowStart := len(runes) - my.overlap - 50
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := len(runes) - my.overlap + 50
		if windowEnd > len(runes) {
			windowEnd = len(runes)
		}

		windowRunes := runes[windowStart:windowEnd]
		idealIdx := len(runes) - windowStart - my.overlap

		var locations []int
		for ri, r := range windowRunes {
			if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' {
				locations = append(locations, ri)
			}
		}

		if len(locations) == 0 {
			continue
		}

		bestRuneIdx := locations[0]
		bestDist := abs(bestRuneIdx - idealIdx)
		for _, loc := range locations {
			dist := abs(loc - idealIdx)
			if dist < bestDist {
				bestDist = dist
				bestRuneIdx = loc
			}
		}

		snapRunePos := windowStart + bestRuneIdx + 1
		if snapRunePos < 0 {
			snapRunePos = 0
		}
		if snapRunePos > len(runes) {
			snapRunePos = len(runes)
		}

		overlapText := strings.TrimSpace(string(runes[snapRunePos:]))
		if overlapText == "" {
			continue
		}

		newContent := overlapText + "\n\n" + result[i].Content
		if utf8.RuneCountInString(newContent) > my.hardMax {
			continue
		}
		result[i].Content = newContent
		result[i].TokenCount = estimateTokens(newContent)
	}

	return result
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func estimateTokens(text string) int {
	ascii := 0
	cjk := 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			cjk++
		}
	}
	return ascii/4 + cjk*2
}
```

Key design decisions addressing reviewer issues:
1. **Separator hierarchy has 4 levels** (paragraph → line → sentence → clause) before char fallback, matching the spec exactly.
2. **`splitPreserve` function** keeps delimiters attached to preceding text (sentence punctuation like `。` stays with its sentence).
3. **`addOverlap` uses direct rune iteration** instead of `regexp.FindAllIndex` on `[]rune` (which is invalid Go — `FindAllIndex` takes `[]byte` only). This gives correct rune offsets for CJK text.
4. **`idealIdx` computed relative to `windowStart`** — the ideal overlap boundary position within the window.
5. **`enforceHardMax` does direct rune slicing** instead of re-calling `recursiveSplit` — eliminates infinite recursion risk.
6. **Overlap guard**: if overlap would push chunk above `hardMax`, skip the overlap for that chunk.
7. **Base64 regex uses `[^)]*`** to match any characters in the data, more robust than `[A-Za-z0-9+/=]+`.
8. **Regex pre-compiled in `init()`** — `separatorRes` avoids repeated compilation in the recursive hot path.
9. **Tests use `utf8.RuneCountInString`** to match implementation's rune-based enforcement.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/chunker/ -v`
Expected: All tests PASS

- [ ] **Step 5: Run full project tests**

Run: `go test ./...`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/chunker/markdown.go internal/chunker/markdown_test.go
git commit -m "feat: rewrite chunker with recursive splitting and sentence-aware overlap"
```

---

### Task 3: Update indexer to use new default chunk size

**Files:**
- Modify: `internal/service/indexer.go:33`

- [ ] **Step 7: Change `NewMarkdownChunker(900)` to `NewMarkdownChunker(800)`**

In `internal/service/indexer.go` line 33, change:
```go
chunker:   chunker.NewMarkdownChunker(900),
```
to:
```go
chunker:   chunker.NewMarkdownChunker(800),
```

- [ ] **Step 8: Run full project tests**

Run: `go test ./...`
Expected: All tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/service/indexer.go
git commit -m "refactor: update indexer chunkSize from 900 to 800"
```

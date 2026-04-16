# Chunker v3 Sliding Window Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace recursive-split chunker with QMD-style sliding window algorithm that produces no fragments.

**Architecture:** Single-pass breakpoint scan + sliding window cut with distance-decay scoring. Prepend `\n` for position-0 heading match. Post-processing: hard split, merge small, sentence-aware overlap.

**Tech Stack:** Go standard library only (`regexp`, `strings`, `unicode/utf8`). No new dependencies.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/chunker/chunker.go` | `Chunk` struct (`StartLine`/`EndLine`) + `Chunker` interface (no `title`) |
| `internal/chunker/markdown.go` | `MarkdownChunker` — full rewrite with sliding window |
| `internal/chunker/markdown_test.go` | Full rewrite of tests |
| `internal/service/indexer.go` | Update `createChunks` call site |

---

## Chunk 1: Interface + Tests + Implementation

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
	chunks, err := NewMarkdownChunker(800).Chunk("")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != text {
		t.Fatalf("expected %q, got %q", text, chunks[0].Content)
	}
	if chunks[0].StartLine != 0 {
		t.Fatalf("expected StartLine 0, got %d", chunks[0].StartLine)
	}
}

func TestChunkStartEndLine(t *testing.T) {
	text := "line0\nline1\nline2\nline3"
	chunks, err := NewMarkdownChunker(20).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if chunks[0].StartLine != 0 {
		t.Fatalf("expected first chunk StartLine=0, got %d", chunks[0].StartLine)
	}
	for i, c := range chunks {
		if c.EndLine < c.StartLine {
			t.Fatalf("chunk %d: EndLine %d < StartLine %d", i, c.EndLine, c.StartLine)
		}
	}
}

func TestChunkRespectsHardMax(t *testing.T) {
	text := strings.Repeat("a", 2000)
	chunks, err := NewMarkdownChunker(800).Chunk(text)
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

func TestChunkSplitByParagraph(t *testing.T) {
	paraA := strings.Repeat("word ", 200)
	paraB := strings.Repeat("other ", 200)
	text := paraA + "\n\n" + paraB
	chunks, err := NewMarkdownChunker(800).Chunk(text)
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

func TestChunkHeadingBreakpoint(t *testing.T) {
	para := strings.Repeat("content line. ", 60)
	text := para + "\n## Section Two\n\n" + para
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks (split at heading), got %d", len(chunks))
	}
}

func TestChunkCodeFenceNotSplit(t *testing.T) {
	code := strings.Repeat("fmt.Println(\"hello\")\n", 30)
	text := "Before.\n\n```go\n" + code + "```\n\nAfter."
	chunks, err := NewMarkdownChunker(200).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "```") && strings.Count(c.Content, "```")%2 != 0 {
			t.Fatalf("code block was split across chunks: %q", c.Content[:50])
		}
	}
}

func TestChunkNoTinyFragments(t *testing.T) {
	text := "Title\n\n---\n\nA\n\nB\n\n" + strings.Repeat("Real content here. ", 50)
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) < 50 {
			t.Fatalf("chunk %d is too small (%d runes): %q", i, utf8.RuneCountInString(c.Content), c.Content)
		}
	}
}

func TestChunkOverlapDoesNotExceedHardMax(t *testing.T) {
	para := strings.Repeat("This is a test sentence. ", 80)
	text := para + "\n\n" + para
	chunks, err := NewMarkdownChunker(800).Chunk(text)
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
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "AAAA") {
			t.Fatal("base64 data should be stripped from chunks")
		}
	}
}

func TestChunkCJKTokenEstimation(t *testing.T) {
	text := "你好世界"
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].TokenCount != 8 {
		t.Fatalf("expected 8 tokens for 4 CJK chars, got %d", chunks[0].TokenCount)
	}
}

func TestNewMarkdownChunkerDefault(t *testing.T) {
	c := NewMarkdownChunker(0)
	if c.chunkSize != 800 {
		t.Fatalf("expected default chunkSize=800, got %d", c.chunkSize)
	}
}

func TestChunkTitleAtPosition0(t *testing.T) {
	text := "# First Heading\n\nSome content here."
	chunks, err := NewMarkdownChunker(800).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "# First Heading") {
		t.Fatalf("expected heading in chunk, got %q", chunks[0].Content)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/chunker/ -v`
Expected: Multiple failures (new interface doesn't match old code)

---

### Task 2: Implement the sliding window chunker

**Files:**
- Rewrite: `internal/chunker/markdown.go`

- [ ] **Step 3: Implement the new MarkdownChunker**

```go
package chunker

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type MarkdownChunker struct {
	chunkSize    int
	hardMax      int
	overlapChars int
	windowChars  int
	decayFactor  float64
	minChunkSize int
}

func NewMarkdownChunker(chunkSize int) *MarkdownChunker {
	if chunkSize <= 0 {
		chunkSize = 800
	}
	return &MarkdownChunker{
		chunkSize:    chunkSize,
		hardMax:      chunkSize + 200,
		overlapChars: 100,
		windowChars:  200,
		decayFactor:  0.7,
		minChunkSize: 100,
	}
}

var base64ImgRe = regexp.MustCompile(`!\[[^\]]*\]\(data:image/[^;]+;base64,[^)]*\)`)

type breakPoint struct {
	pos   int
	score int
}

type codeFence struct {
	start int
	end   int
}

func scanBreakPoints(text string) []breakPoint {
	type pattern struct {
		re    *regexp.Regexp
		score int
	}
	patterns := []pattern{
		{regexp.MustCompile(`\n#{1}(?!#)`), 100},
		{regexp.MustCompile(`\n#{2}(?!#)`), 90},
		{regexp.MustCompile(`\n#{3}(?!#)`), 80},
		{regexp.MustCompile(`\n#{4}(?!#)`), 70},
		{regexp.MustCompile(`\n```[^\n]*`), 80},
		{regexp.MustCompile(`\n(?:---|\*\*\*|___)\s*\n`), 60},
		{regexp.MustCompile(`\n\n+`), 20},
	}

	seen := make(map[int]int)
	for _, p := range patterns {
		locs := p.re.FindAllStringIndex(text, -1)
		for _, loc := range locs {
			pos := loc[0]
			if existing, ok := seen[pos]; !ok || p.score > existing {
				seen[pos] = p.score
			}
		}
	}

	points := make([]breakPoint, 0, len(seen))
	for pos, score := range seen {
		points = append(points, breakPoint{pos: pos, score: score})
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].pos < points[j].pos
	})
	return points
}

func scanCodeFences(text string) []codeFence {
	re := regexp.MustCompile("```[^\n]*\n")
	var fences []codeFence
	inCode := false
	var start int
	locs := re.FindAllStringIndex(text, -1)
	for _, loc := range locs {
		if !inCode {
			start = loc[0]
			inCode = true
		} else {
			fences = append(fences, codeFence{start: start, end: loc[1]})
			inCode = false
		}
	}
	if inCode {
		fences = append(fences, codeFence{start: start, end: len(text)})
	}
	return fences
}

func isInsideCodeFence(pos int, fences []codeFence) bool {
	for _, f := range fences {
		if pos > f.start && pos < f.end {
			return true
		}
		if f.start > pos {
			break
		}
	}
	return false
}

func findBestCutoff(points []breakPoint, targetPos int, windowChars int, decayFactor float64, fences []codeFence) int {
	windowStart := targetPos - windowChars
	bestPos := targetPos
	bestScore := 0.0

	for _, bp := range points {
		if bp.pos < windowStart {
			continue
		}
		if bp.pos > targetPos {
			break
		}
		if isInsideCodeFence(bp.pos, fences) {
			continue
		}
		distance := float64(targetPos - bp.pos)
		normalizedDist := distance / float64(windowChars)
		multiplier := 1.0 - (normalizedDist * normalizedDist * decayFactor)
		finalScore := float64(bp.score) * multiplier
		if finalScore > bestScore {
			bestScore = finalScore
			bestPos = bp.pos
		}
	}
	return bestPos
}

func lineStartPositions(text string) []int {
	positions := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			positions = append(positions, i+1)
		}
	}
	return positions
}

func byteOffsetToLine(offset int, lineStarts []int) int {
	lo, hi := 0, len(lineStarts)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lineStarts[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}

func (my *MarkdownChunker) Chunk(body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	body = my.stripBase64Images(body)
	prepended := "\n" + body

	points := scanBreakPoints(prepended)
	fences := scanCodeFences(prepended)
	lineStarts := lineStartPositions(prepended)

	var rawChunks []struct {
		content string
		start   int
		end     int
	}

	charPos := 0
	runeCount := utf8.RuneCountInString(prepended)

	for charPos < runeCount {
		targetEnd := charPos + my.chunkSize
		if targetEnd >= runeCount {
			rawChunks = append(rawChunks, struct {
				content string
				start   int
				end     int
			}{content: prepended[charPos:], start: charPos, end: runeCount})
			break
		}

		bestBreak := findBestCutoff(points, targetEnd, my.windowChars, my.decayFactor, fences)
		if bestBreak <= charPos {
			bestBreak = targetEnd
		}

		rawChunks = append(rawChunks, struct {
			content string
			start   int
			end     int
		}{content: prepended[charPos:bestBreak], start: charPos, end: bestBreak})

		charPos = bestBreak
	}

	rawChunks = my.hardSplit(rawChunks)
	rawChunks = my.mergeSmall(rawChunks)
	chunks := my.buildChunks(rawChunks, lineStarts)
	chunks = my.addOverlap(chunks)
	return chunks, nil
}

func (my *MarkdownChunker) stripBase64Images(body string) string {
	return base64ImgRe.ReplaceAllString(body, "...(truncated)")
}

func (my *MarkdownChunker) hardSplit(rawChunks []struct {
	content string
	start   int
	end     int
}) []struct {
	content string
	start   int
	end     int
} {
	var result []struct {
		content string
		start   int
		end     int
	}
	for _, rc := range rawChunks {
		runes := []rune(rc.content)
		if len(runes) <= my.hardMax {
			result = append(result, rc)
			continue
		}
		offset := rc.start
		for i := 0; i < len(runes); i += my.hardMax {
			end := i + my.hardMax
			if end > len(runes) {
				end = len(runes)
			}
			part := string(runes[i:end])
			result = append(result, struct {
				content string
				start   int
				end     int
			}{content: part, start: offset, end: offset + end - i})
			offset += end - i
		}
	}
	return result
}

func (my *MarkdownChunker) mergeSmall(rawChunks []struct {
	content string
	start   int
	end     int
}) []struct {
	content string
	start   int
	end     int
} {
	if len(rawChunks) <= 1 {
		return rawChunks
	}

	var merged []struct {
		content string
		start   int
		end     int
	}
	current := rawChunks[0]

	for i := 1; i < len(rawChunks); i++ {
		rc := rawChunks[i]
		curRunes := utf8.RuneCountInString(current.content)
		rcRunes := utf8.RuneCountInString(rc.content)

		if curRunes < my.minChunkSize {
			combined := current.content + "\n" + rc.content
			if utf8.RuneCountInString(combined) <= my.hardMax {
				current.content = combined
				current.end = rc.end
				continue
			}
		}

		if rcRunes < my.minChunkSize && merged == nil && curRunes+1+rcRunes <= my.hardMax {
			combined := current.content + "\n" + rc.content
			if utf8.RuneCountInString(combined) <= my.hardMax {
				current.content = combined
				current.end = rc.end
				continue
			}
		}

		merged = append(merged, current)
		current = rc
	}
	merged = append(merged, current)

	if utf8.RuneCountInString(merged[0].content) < my.minChunkSize && len(merged) > 1 {
		combined := merged[0].content + "\n" + merged[1].content
		if utf8.RuneCountInString(combined) <= my.hardMax {
			merged[1] = struct {
				content string
				start   int
				end     int
			}{content: combined, start: merged[0].start, end: merged[1].end}
			merged = merged[1:]
		}
	}

	return merged
}

func (my *MarkdownChunker) buildChunks(rawChunks []struct {
	content string
	start   int
	end     int
}, lineStarts []int) []Chunk {
	chunks := make([]Chunk, 0, len(rawChunks))
	for _, rc := range rawChunks {
		content := strings.TrimSpace(rc.content)
		if content == "" {
			continue
		}
		startLine := byteOffsetToLine(rc.start, lineStarts)
		endLine := byteOffsetToLine(rc.end-1, lineStarts)
		if startLine > 0 {
			startLine--
		}
		if endLine > 0 {
			endLine--
		}
		chunks = append(chunks, Chunk{
			Content:    content,
			StartLine:  startLine,
			EndLine:    endLine,
			TokenCount: estimateTokens(content),
		})
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

		if len(runes) <= my.overlapChars {
			continue
		}

		windowStart := len(runes) - my.overlapChars - 50
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := len(runes) - my.overlapChars + 50
		if windowEnd > len(runes) {
			windowEnd = len(runes)
		}

		windowRunes := runes[windowStart:windowEnd]
		idealIdx := len(runes) - windowStart - my.overlapChars

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/chunker/ -v`
Expected: All tests PASS

- [ ] **Step 5: Fix any test failures, then run again**

Run: `go test ./internal/chunker/ -v`
Expected: All tests PASS

---

### Task 3: Update indexer call site

**Files:**
- Modify: `internal/service/indexer.go`

- [ ] **Step 6: Update createChunks to use new interface**

Change `createChunks` method — remove `title` param, use `StartLine` for `Position`:

```go
func (idx *Indexer) createChunks(docId int64, body, hash string) error {
	chunks, err := idx.chunker.Chunk(body)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	data := make([]dao.ChunkData, len(chunks))
	tokenized := make([]string, len(chunks))
	for i, c := range chunks {
		data[i] = dao.ChunkData{
			Content:    c.Content,
			Position:   c.StartLine,
			TokenCount: c.TokenCount,
			Hash:       hash,
		}
		tokenized[i] = idx.tokenizer.TokenizeToString(c.Content)
	}
	_, err = dao.InsertChunks(docId, data, tokenized)
	return err
}
```

Update the caller at line ~120 to pass `body` and `hash` only (remove `title`).

- [ ] **Step 7: Run full project build**

Run: `go build ./...`
Expected: Compiles successfully

- [ ] **Step 8: Commit**

```bash
git add internal/chunker/ internal/service/indexer.go
git commit -m "feat: rewrite chunker with sliding window algorithm (v3)"
```

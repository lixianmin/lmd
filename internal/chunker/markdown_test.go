package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkEmptyBody(t *testing.T) {
	chunks, err := NewMarkdownChunker(300).Chunk("")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].Content != text {
		t.Fatalf("expected %q, got %q", text, chunks[0].Content)
	}
	if chunks[0].StartLine != 0 {
		t.Fatalf("expected StartLine 0, got %d", chunks[0].StartLine)
	}
}

func TestChunkStartEndLine(t *testing.T) {
	text := strings.Repeat("hello world this is a test line\n", 20)
	chunks, err := NewMarkdownChunker(50).Chunk(text)
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
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for 2000-char text, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 450 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkSplitByParagraph(t *testing.T) {
	paraA := strings.Repeat("word ", 200)
	paraB := strings.Repeat("other ", 200)
	text := paraA + "\n\n" + paraB
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 450 {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkHeadingBreakpoint(t *testing.T) {
	para := strings.Repeat("content line. ", 60)
	text := para + "\n## Section Two\n\n" + para
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks (split at heading), got %d", len(chunks))
	}
}

func TestChunkCodeFenceNotSplit(t *testing.T) {
	code := strings.Repeat("fmt.Println(\"hello\")\n", 10)
	text := "Before.\n\n```go\n" + code + "```\n\nAfter."
	chunks, err := NewMarkdownChunker(200).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		fenceCount := strings.Count(c.Content, "```")
		if fenceCount%2 != 0 {
			t.Fatalf("code block was split: odd fence count %d in chunk", fenceCount)
		}
	}
}

func TestChunkNoTinyFragments(t *testing.T) {
	text := "Title\n\n---\n\nA\n\nB\n\n" + strings.Repeat("Real content here. ", 50)
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) < 20 {
			t.Fatalf("chunk %d is too small (%d runes): %q", i, utf8.RuneCountInString(c.Content), c.Content)
		}
	}
}

func TestChunkOverlapDoesNotExceedHardMax(t *testing.T) {
	para := strings.Repeat("This is a test sentence. ", 80)
	text := para + "\n\n" + para
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > 450 {
			t.Fatalf("chunk %d with overlap exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestChunkStripsBase64Images(t *testing.T) {
	text := "# Title\n\nSome text here.\n\n![img](data:image/png;base64," + strings.Repeat("A", 5000) + ")\n\nMore text."
	chunks, err := NewMarkdownChunker(300).Chunk(text)
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
	chunks, err := NewMarkdownChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].TokenCount != 8 {
		t.Fatalf("expected 8 tokens for 4 CJK chars, got %d", chunks[0].TokenCount)
	}
}

func TestNewMarkdownChunkerDefault(t *testing.T) {
	c := NewMarkdownChunker(0)
	if c.chunkSize != 300 {
		t.Fatalf("expected default chunkSize=300, got %d", c.chunkSize)
	}
	if c.overlapChars != 45 {
		t.Fatalf("expected default overlapChars=45, got %d", c.overlapChars)
	}
}

func TestChunkTitleAtPosition0(t *testing.T) {
	text := "# First Heading\n\nSome content here."
	chunks, err := NewMarkdownChunker(300).Chunk(text)
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

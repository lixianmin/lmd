package chunker

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlainChunkEmptyBody(t *testing.T) {
	chunks, err := NewPlainTextChunker(300).Chunk("")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestPlainChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewPlainTextChunker(300).Chunk(text)
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

func TestPlainChunkSplitsLongText(t *testing.T) {
	text := strings.Repeat("abcdefghij", 100)
	chunks, err := NewPlainTextChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for 1000-char text, got %d", len(chunks))
	}
	totalRunes := 0
	for _, c := range chunks {
		totalRunes += utf8.RuneCountInString(c.Content)
	}
	if totalRunes < 900 {
		t.Fatalf("expected total runes >= 900, got %d", totalRunes)
	}
}

func TestPlainChunkRespectsHardMax(t *testing.T) {
	text := strings.Repeat("a", 2000)
	chunks, err := NewPlainTextChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	hardMax := 300 + 300/2
	for i, c := range chunks {
		if utf8.RuneCountInString(c.Content) > hardMax {
			t.Fatalf("chunk %d exceeds hardMax: runeCount=%d", i, utf8.RuneCountInString(c.Content))
		}
	}
}

func TestPlainChunkSentenceBoundary(t *testing.T) {
	sentences := make([]string, 50)
	for i := range sentences {
		sentences[i] = "This is sentence number " + string(rune('0'+i%10)) + "."
	}
	text := strings.Join(sentences, " ")

	chunks, err := NewPlainTextChunker(300).Chunk(text)
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

func TestPlainChunkCJKSentenceBoundary(t *testing.T) {
	sentences := make([]string, 60)
	for i := range sentences {
		sentences[i] = "这是第" + string(rune('零'+i%10)) + "句话。"
	}
	text := strings.Join(sentences, "")

	chunks, err := NewPlainTextChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for long CJK text, got %d", len(chunks))
	}
}

func TestPlainChunkNewlineBoundary(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "Line number " + string(rune('0'+i%10))
	}
	text := strings.Join(lines, "\n")

	chunks, err := NewPlainTextChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestPlainChunkMergesTinyTail(t *testing.T) {
	longPart := strings.Repeat("ab", 200)
	text := longPart + "xy"
	chunks, err := NewPlainTextChunker(300).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	lastChunk := chunks[len(chunks)-1]
	if utf8.RuneCountInString(lastChunk.Content) < 75 {
		t.Fatalf("last chunk too small (%d runes), should have been merged", utf8.RuneCountInString(lastChunk.Content))
	}
}

func TestPlainChunkStartEndLine(t *testing.T) {
	text := strings.Repeat("hello world\n", 60)
	chunks, err := NewPlainTextChunker(100).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.EndLine < c.StartLine {
			t.Fatalf("chunk %d: EndLine %d < StartLine %d", i, c.EndLine, c.StartLine)
		}
	}
}

func TestPlainChunkLineNumbersNotOffByOne(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, "line "+fmt.Sprintf("%02d", i))
	}
	text := strings.Join(lines, "\n")

	chunks, err := NewPlainTextChunker(100).Chunk(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	first := chunks[0]
	if first.StartLine != 0 {
		t.Fatalf("expected first chunk StartLine=0, got %d", first.StartLine)
	}

	for i, c := range chunks {
		if c.EndLine < c.StartLine {
			t.Fatalf("chunk %d: EndLine %d < StartLine %d", i, c.EndLine, c.StartLine)
		}
	}

	if chunks[0].EndLine <= 0 {
		t.Fatalf("expected first chunk EndLine > 0, got %d", chunks[0].EndLine)
	}

	lastChunk := chunks[len(chunks)-1]
	if lastChunk.EndLine < 55 {
		t.Fatalf("expected last chunk EndLine >= 55, got %d", lastChunk.EndLine)
	}
}

func TestPlainChunkDefaultSize(t *testing.T) {
	c := NewPlainTextChunker(0)
	if c.chunkSize != 300 {
		t.Fatalf("expected default chunkSize=300, got %d", c.chunkSize)
	}
	if c.overlapChars != 45 {
		t.Fatalf("expected default overlapChars=45, got %d", c.overlapChars)
	}
}

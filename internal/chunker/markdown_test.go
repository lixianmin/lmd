package chunker

import (
	"strings"
	"testing"
)

func TestChunkByHeading(t *testing.T) {
	text := "# Title\n\nParagraph one.\n\n## Section A\n\nContent A.\n\n## Section B\n\nContent B."
	chunks, err := NewMarkdownChunker(20).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for multi-section doc, got %d", len(chunks))
	}
}

func TestChunkShortDocument(t *testing.T) {
	text := "Short document."
	chunks, err := NewMarkdownChunker(900).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short doc, got %d", len(chunks))
	}
}

func TestChunkEmptyDocument(t *testing.T) {
	chunks, err := NewMarkdownChunker(900).Chunk("", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty doc, got %d", len(chunks))
	}
}

func TestChunkRespectsCodeBlocks(t *testing.T) {
	code := strings.Repeat("line\n", 100)
	text := "# Code\n\n```go\n" + code + "```\n\n## After\n\nMore content."
	chunks, err := NewMarkdownChunker(200).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, "```") && strings.Count(c.Content, "```")%2 != 0 {
			t.Fatal("code block was split across chunks")
		}
	}
}

func TestChunkPosition(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph."
	chunks, err := NewMarkdownChunker(900).Chunk("", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) > 0 && chunks[0].Position != 0 {
		t.Fatalf("expected first chunk position=0, got %d", chunks[0].Position)
	}
}

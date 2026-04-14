package formatter

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

var sampleHits = []SearchHit{
	{DocId: "abc123", Collection: "notes", Path: "go.md", Title: "Go并发", Score: 0.95, Snippet: "goroutine and channel", Line: 42},
	{DocId: "def456", Collection: "notes", Path: "python.md", Title: "Python", Score: 0.80, Snippet: "pandas dataframe", Line: 10},
}

func TestTextFormatter(t *testing.T) {
	f := NewTextFormatter(TextConfig{Full: false})
	var buf bytes.Buffer
	if err := f.Format(&buf, sampleHits); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "go.md:42") {
		t.Fatalf("expected 'go.md:42' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "#abc123") {
		t.Fatalf("expected '#abc123' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Title: Go并发") {
		t.Fatalf("expected 'Title: Go并发' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "95%") {
		t.Fatalf("expected '95%%' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "goroutine and channel") {
		t.Fatalf("expected snippet in output, got:\n%s", out)
	}
}

func TestTextFormatterFull(t *testing.T) {
	f := NewTextFormatter(TextConfig{Full: true})
	var buf bytes.Buffer
	if err := f.Format(&buf, sampleHits); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "goroutine and channel") {
		t.Fatal("expected snippet in full output")
	}
}

func TestTextFormatterEmpty(t *testing.T) {
	f := NewTextFormatter(TextConfig{})
	var buf bytes.Buffer
	if err := f.Format(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No results found") {
		t.Fatalf("expected 'No results found', got %q", buf.String())
	}
}

func TestJSONFormatter(t *testing.T) {
	f := NewJSONFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, sampleHits); err != nil {
		t.Fatal(err)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	if result[0]["doc_id"] != "abc123" {
		t.Fatalf("expected doc_id 'abc123', got %v", result[0]["doc_id"])
	}
	if result[0]["score"] != 0.95 {
		t.Fatalf("expected score 0.95, got %v", result[0]["score"])
	}
}

func TestJSONFormatterEmpty(t *testing.T) {
	f := NewJSONFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Fatalf("expected '[]\\n', got %q", buf.String())
	}
}

func TestJSONFormatterOmitEmptyCollection(t *testing.T) {
	f := NewJSONFormatter()
	hits := []SearchHit{
		{DocId: "abc", Path: "go.md", Title: "Go", Score: 0.9, Snippet: "hi", Line: 1},
	}
	var buf bytes.Buffer
	f.Format(&buf, hits)

	var result []map[string]interface{}
	json.Unmarshal(buf.Bytes(), &result)
	if _, ok := result[0]["collection"]; ok {
		t.Fatal("expected collection to be omitted when empty")
	}
}

func TestCSVFormatter(t *testing.T) {
	f := NewCSVFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, sampleHits); err != nil {
		t.Fatal(err)
	}

	reader := csv.NewReader(&buf)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("invalid CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 rows (header + 2 data), got %d", len(records))
	}
	if records[0][0] != "path" {
		t.Fatalf("expected header 'path', got '%s'", records[0][0])
	}
	if records[1][0] != "go.md" {
		t.Fatalf("expected 'go.md', got '%s'", records[1][0])
	}
	if records[1][4] != "95.00" {
		t.Fatalf("expected score '95.00', got '%s'", records[1][4])
	}
}

func TestCSVFormatterEmpty(t *testing.T) {
	f := NewCSVFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, nil); err != nil {
		t.Fatal(err)
	}

	reader := csv.NewReader(&buf)
	records, _ := reader.ReadAll()
	if len(records) != 1 {
		t.Fatalf("expected header only (1 row), got %d", len(records))
	}
}

func TestCSVFormatterTruncatesLongSnippet(t *testing.T) {
	f := NewCSVFormatter()
	longSnippet := strings.Repeat("x", 600)
	hits := []SearchHit{
		{DocId: "a", Path: "f.md", Title: "T", Score: 0.5, Snippet: longSnippet, Line: 1},
	}
	var buf bytes.Buffer
	f.Format(&buf, hits)

	reader := csv.NewReader(&buf)
	records, _ := reader.ReadAll()
	if len(records[1][5]) != 500 {
		t.Fatalf("expected snippet truncated to 500, got %d", len(records[1][5]))
	}
}

func TestMarkdownFormatter(t *testing.T) {
	f := NewMarkdownFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, sampleHits); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "### 1. Go并发") {
		t.Fatalf("expected '### 1. Go并发', got:\n%s", out)
	}
	if !strings.Contains(out, "`go.md`") {
		t.Fatalf("expected '`go.md`', got:\n%s", out)
	}
	if !strings.Contains(out, "#abc123") {
		t.Fatalf("expected '#abc123', got:\n%s", out)
	}
	if !strings.Contains(out, "---") {
		t.Fatalf("expected '---' separator, got:\n%s", out)
	}
}

func TestMarkdownFormatterEmpty(t *testing.T) {
	f := NewMarkdownFormatter()
	var buf bytes.Buffer
	if err := f.Format(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No results found") {
		t.Fatalf("expected 'No results found', got %q", buf.String())
	}
}

func TestMarkdownFormatterFallsBackToPathForTitle(t *testing.T) {
	f := NewMarkdownFormatter()
	hits := []SearchHit{
		{DocId: "x", Path: "no-title.md", Title: "", Score: 0.5, Snippet: "content", Line: 1},
	}
	var buf bytes.Buffer
	f.Format(&buf, hits)
	if !strings.Contains(buf.String(), "### 1. no-title.md") {
		t.Fatalf("expected path as fallback title, got:\n%s", buf.String())
	}
}

func TestMarkdownFormatterStripsTrailingNewline(t *testing.T) {
	f := NewMarkdownFormatter()
	hits := []SearchHit{
		{DocId: "x", Path: "a.md", Title: "A", Score: 0.5, Snippet: "hello\n", Line: 1},
	}
	var buf bytes.Buffer
	f.Format(&buf, hits)
	if strings.Contains(buf.String(), "hello\n\n```") {
		t.Fatal("trailing newline in snippet should be stripped")
	}
}

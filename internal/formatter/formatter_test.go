package formatter

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextFormatter(t *testing.T) {
	hits := []SearchHit{
		{DocId: "abc", Collection: "notes", Path: "go.md", Title: "Go并发", Score: 0.95, Snippet: "goroutine...", Line: 42},
		{DocId: "def", Collection: "notes", Path: "python.md", Title: "Python", Score: 0.80, Snippet: "pandas...", Line: 10},
	}
	f := NewTextFormatter(TextConfig{Full: false})
	var buf bytes.Buffer
	err := f.Format(&buf, hits)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "go.md") {
		t.Fatal("expected go.md in output")
	}
	if !strings.Contains(out, "95%") {
		t.Fatal("expected 95% in output")
	}
}

func TestTextFormatterFull(t *testing.T) {
	hits := []SearchHit{
		{DocId: "abc", Path: "go.md", Title: "Go", Score: 0.9, Snippet: "hello world", Line: 1},
	}
	f := NewTextFormatter(TextConfig{Full: true})
	var buf bytes.Buffer
	_ = f.Format(&buf, hits)
	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Fatal("expected snippet in full output")
	}
}

func TestTextFormatterEmpty(t *testing.T) {
	f := NewTextFormatter(TextConfig{})
	var buf bytes.Buffer
	err := f.Format(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No results") {
		t.Fatal("expected 'No results' for empty hits")
	}
}

func TestJSONFormatter(t *testing.T) {
	hits := []SearchHit{
		{DocId: "abc", Path: "go.md", Title: "Go", Score: 0.9, Snippet: "hello", Line: 1},
	}
	f := NewJSONFormatter()
	var buf bytes.Buffer
	err := f.Format(&buf, hits)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"path"`) {
		t.Fatal("expected JSON with path field")
	}
	if !strings.Contains(out, `"doc_id"`) {
		t.Fatal("expected JSON with doc_id field")
	}
}

func TestJSONFormatterEmpty(t *testing.T) {
	f := NewJSONFormatter()
	var buf bytes.Buffer
	err := f.Format(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Fatalf("expected '[]' for empty hits, got %q", buf.String())
	}
}

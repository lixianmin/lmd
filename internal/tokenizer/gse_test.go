package tokenizer

import (
	"strings"
	"testing"
)

func newTestTokenizer(t *testing.T) *GseTokenizer {
	t.Helper()
	tok, err := NewGseTokenizer()
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}
	return tok
}

func TestCutChinese(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("搜索引擎支持中文检索")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for Chinese text")
	}
	joined := strings.Join(tokens, " ")
	if !strings.Contains(joined, "搜索") || !strings.Contains(joined, "引擎") {
		t.Fatalf("expected key tokens, got: %v", tokens)
	}
}

func TestCutEnglish(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("Hello World, this is a test")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for English text")
	}
}

func TestCutMixed(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("Go语言实现搜索引擎")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for mixed text")
	}
	joined := strings.Join(tokens, " ")
	if !strings.Contains(joined, "搜索") || !strings.Contains(joined, "引擎") {
		t.Fatalf("expected key Chinese tokens, got: %v", tokens)
	}
}

func TestCutEmpty(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("")
	if len(tokens) != 0 {
		t.Fatalf("expected empty result for empty input, got: %v", tokens)
	}
}

func TestCutForSearch(t *testing.T) {
	tok := newTestTokenizer(t)
	normal := tok.Cut("搜索引擎")
	search := tok.CutForSearch("搜索引擎")
	if len(search) < len(normal) {
		t.Fatalf("search mode should produce at least as many tokens as normal mode, got normal=%d search=%d", len(normal), len(search))
	}
}

func TestTokenizeToString(t *testing.T) {
	tok := newTestTokenizer(t)
	result := tok.TokenizeToString("搜索引擎支持中文")
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	parts := strings.Split(result, " ")
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 space-separated tokens, got: %s", result)
	}
}

func TestTokenizeToStringEmpty(t *testing.T) {
	tok := newTestTokenizer(t)
	result := tok.TokenizeToString("")
	if result != "" {
		t.Fatalf("expected empty string for empty input, got: %s", result)
	}
}

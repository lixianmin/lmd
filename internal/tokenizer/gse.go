package tokenizer

import (
	"strings"

	"github.com/go-ego/gse"
)

type GseTokenizer struct {
	seg *gse.Segmenter
}

func NewGseTokenizer() (*GseTokenizer, error) {
	var seg gse.Segmenter
	seg.SkipLog = true
	if err := seg.LoadDict("zh"); err != nil {
		return nil, err
	}
	return &GseTokenizer{seg: &seg}, nil
}

func (t *GseTokenizer) Cut(text string) []string {
	if text == "" {
		return nil
	}
	return t.seg.Cut(text)
}

func (t *GseTokenizer) CutForSearch(text string) []string {
	if text == "" {
		return nil
	}
	return t.seg.CutSearch(text)
}

func (t *GseTokenizer) TokenizeToString(text string) string {
	tokens := t.Cut(text)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}

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

func (my *GseTokenizer) Cut(text string) []string {
	if text == "" {
		return nil
	}
	return my.seg.Cut(text)
}

func (my *GseTokenizer) CutForSearch(text string) []string {
	if text == "" {
		return nil
	}
	return my.seg.CutSearch(text)
}

func (my *GseTokenizer) TokenizeToString(text string) string {
	tokens := my.Cut(text)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}

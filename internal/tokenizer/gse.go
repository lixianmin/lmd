package tokenizer

import (
	"strings"
	"sync"

	"github.com/go-ego/gse"
)

type GseTokenizer struct {
	mu  sync.Mutex
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
	my.mu.Lock()
	defer my.mu.Unlock()
	return my.seg.Cut(text)
}

func (my *GseTokenizer) CutForSearch(text string) []string {
	if text == "" {
		return nil
	}
	my.mu.Lock()
	defer my.mu.Unlock()
	return my.seg.CutSearch(text)
}

func (my *GseTokenizer) TokenizeToString(text string) string {
	tokens := my.Cut(text)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}

func (my *GseTokenizer) Pos(text string) []SegPos {
	if text == "" {
		return nil
	}
	my.mu.Lock()
	defer my.mu.Unlock()
	loadEnPos()
	gseResults := my.seg.Pos(text)
	results := make([]SegPos, 0, len(gseResults))
	for _, sp := range gseResults {
		pos := sp.Pos
		if pos == "x" || pos == "eng" {
			if enPos, ok := enPosMap[strings.ToLower(sp.Text)]; ok {
				pos = enPos
			}
		}
		results = append(results, SegPos{Text: sp.Text, Pos: pos})
	}
	return results
}

package formatter

import (
	"encoding/json"
	"io"
)

type jsonHit struct {
	DocId      string  `json:"doc_id"`
	Collection string  `json:"collection,omitempty"`
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Score      float64 `json:"score"`
	Snippet    string  `json:"snippet"`
	Line       int     `json:"line"`
}

type JSONFormatter struct{}

func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

func (my *JSONFormatter) Format(w io.Writer, hits []SearchHit) error {
	if hits == nil {
		hits = []SearchHit{}
	}
	out := make([]jsonHit, len(hits))
	for i, h := range hits {
		out[i] = jsonHit{
			DocId:      h.DocId,
			Collection: h.Collection,
			Path:       h.Path,
			Title:      h.Title,
			Score:      h.Score,
			Snippet:    h.Snippet,
			Line:       h.Line,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

package formatter

import (
	"encoding/csv"
	"fmt"
	"io"
)

type CSVFormatter struct{}

func NewCSVFormatter() *CSVFormatter {
	return &CSVFormatter{}
}

func (f *CSVFormatter) Format(w io.Writer, hits []SearchHit) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	cw.Write([]string{"path", "line", "doc_id", "title", "score", "snippet"})

	for _, r := range hits {
		snippet := r.Snippet
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		cw.Write([]string{
			r.Path,
			fmt.Sprintf("%d", r.Line),
			r.DocID,
			r.Title,
			fmt.Sprintf("%.2f", r.Score*100),
			snippet,
		})
	}

	return cw.Error()
}

package formatter

import (
	"encoding/csv"
	"fmt"
	"io"
)

const csvSnippetMaxRunes = 500 // CSV 输出 snippet 最大 rune 数

type CSVFormatter struct{}

func NewCSVFormatter() *CSVFormatter {
	return &CSVFormatter{}
}

func (my *CSVFormatter) Format(w io.Writer, hits []SearchHit) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	cw.Write([]string{"path", "line", "doc_id", "title", "score", "snippet"})

	for _, r := range hits {
		snippet := r.Snippet
		if len(snippet) > csvSnippetMaxRunes {
			snippet = snippet[:csvSnippetMaxRunes]
		}
		cw.Write([]string{
			r.Path,
			fmt.Sprintf("%d", r.Line),
			r.DocId,
			r.Title,
			fmt.Sprintf("%.2f", r.Score*100),
			snippet,
		})
	}

	return cw.Error()
}

package formatter

import (
	"fmt"
	"io"
)

type TextConfig struct {
	Full bool
}

type TextFormatter struct {
	config TextConfig
}

func NewTextFormatter(config TextConfig) *TextFormatter {
	return &TextFormatter{config: config}
}

func (f *TextFormatter) Format(w io.Writer, hits []SearchHit) error {
	if len(hits) == 0 {
		fmt.Fprintln(w, "No results found.")
		return nil
	}

	for _, r := range hits {
		fmt.Fprintf(w, "%s:%d #%s\n", r.Path, r.Line, r.DocId)
		fmt.Fprintf(w, "Title: %s\n", r.Title)
		fmt.Fprintf(w, "Score: %.0f%%\n", r.Score*100)
		if f.config.Full {
			fmt.Fprintln(w)
			fmt.Fprintln(w, r.Snippet)
		} else {
			fmt.Fprintf(w, "\n%s\n", r.Snippet)
		}
		fmt.Fprintln(w)
	}
	return nil
}

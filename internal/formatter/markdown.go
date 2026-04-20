package formatter

import (
	"fmt"
	"io"
	"strings"
)

type MarkdownFormatter struct{}

func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

func (my *MarkdownFormatter) Format(w io.Writer, hits []SearchHit) error {
	if len(hits) == 0 {
		fmt.Fprintln(w, "No results found.")
		return nil
	}

	for i, r := range hits {
		if i > 0 {
			fmt.Fprintln(w, "---")
		}
		title := r.Title
		if title == "" {
			title = r.Path
		}
		fmt.Fprintf(w, "### %d. %s\n", i+1, title)
		fmt.Fprintf(w, "- **Path**: `%s`\n", r.Path)
		fmt.Fprintf(w, "- **Score**: %.0f%%\n", r.Score*100)
		fmt.Fprintf(w, "- **ID**: #%s\n", r.DocId)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```")
		fmt.Fprintln(w, strings.TrimSuffix(r.Snippet, "\n"))
		fmt.Fprintln(w, "```")
		fmt.Fprintln(w)
	}
	return nil
}

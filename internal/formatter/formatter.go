package formatter

import "io"

type SearchHit struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Formatter interface {
	Format(w io.Writer, hits []SearchHit) error
}

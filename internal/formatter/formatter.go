package formatter

import "io"

type SearchHit struct {
	ChunkId    int64
	DocId      string
	DocRowId   int64
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

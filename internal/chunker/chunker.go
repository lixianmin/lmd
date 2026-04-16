package chunker

type Chunk struct {
	Content    string
	StartLine  int
	EndLine    int
	TokenCount int
}

type Chunker interface {
	Chunk(body string) ([]Chunk, error)
}
